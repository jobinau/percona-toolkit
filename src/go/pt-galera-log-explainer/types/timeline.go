package types

import (
	"math"
	"path/filepath"
	"time"
)

// It should be kept already sorted by timestamp
type LocalTimeline []LogInfo

func (lt LocalTimeline) Add(li LogInfo) LocalTimeline {

	// to deduplicate, it will keep 2 loginfo occurrences
	// 1st one for the 1st timestamp found, it will also show the number of repetition
	// 2nd loginfo the keep the last timestamp found, so that we don't loose track
	// so there will be a corner case if the first ever event is repeated, but that is acceptable
	if len(lt) > 1 && li.IsDuplicatedEvent(lt[len(lt)-2], lt[len(lt)-1]) {
		lt[len(lt)-2].RepetitionCount++
		lt[len(lt)-1] = li
	} else {
		lt = append(lt, li)
	}
	return lt
}

// "string" key is a node IP
type Timeline map[string]LocalTimeline

func (timeline Timeline) MergeByIdentifier(lt LocalTimeline) {
	// identify the node with the easiest to read information
	// this is critical part to aggregate logs: this is what enable to merge logs
	// ultimately the "identifier" will be used for columns header
	node := Identifier(lt[len(lt)-1].LogCtx, getlasttime(lt))
	if lt2, ok := timeline[node]; ok {
		lt = MergeTimeline(lt2, lt)
	}
	timeline[node] = lt
}

func (timeline Timeline) MergeByDirectory(path string, lt LocalTimeline) {
	node := filepath.Base(filepath.Dir(path))
	for _, lt2 := range timeline {
		if len(lt2) > 0 && node == filepath.Base(filepath.Dir(lt2[0].LogCtx.FilePath)) {
			lt = MergeTimeline(lt2, lt)
		}
	}
	timeline[node] = lt
}

// MergeTimeline is helpful when log files are split by date, it can be useful to be able to merge content
// a "timeline" come from a log file. Log files that came from some node should not never have overlapping dates
func MergeTimeline(t1, t2 LocalTimeline) LocalTimeline {
	if len(t1) == 0 {
		return t2
	}
	if len(t2) == 0 {
		return t1
	}

	startt1 := getfirsttime(t1)
	startt2 := getfirsttime(t2)

	// just flip them, easier than adding too many nested conditions
	// t1: ---O----?--
	// t2: --O-----?--
	if startt1.After(startt2) {
		return MergeTimeline(t2, t1)
	}

	endt1 := getlasttime(t1)
	endt2 := getlasttime(t2)

	// if t2 is an updated version of t1, or t1 an updated of t2, or t1=t2
	// t1: --O-----?--
	// t2: --O-----?--
	if startt1.Equal(startt2) {
		// t2 > t1
		// t1: ---O---O----
		// t2: ---O-----O--
		if endt1.Before(endt2) {
			return t2
		}
		// t1: ---O-----O--
		// t2: ---O-----O--
		// or
		// t1: ---O-----O--
		// t2: ---O---O----
		return t1
	}

	// if t1 superseds t2
	// t1: --O-----O--
	// t2: ---O---O---
	// or
	// t1: --O-----O--
	// t2: ---O----O--
	if endt1.After(endt2) || endt1.Equal(endt2) {
		return t1
	}
	//return append(t1, t2...)

	// t1: --O----O----
	// t2: ----O----O--
	if endt1.After(startt2) {
		// t1: --O----O----
		// t2: ----OO--OO--
		//>t : --O----OOO-- won't try to get things between t1.end and t2.start
		// we assume they're identical, they're supposed to be from the same server
		t2 = CutTimelineAt(t2, endt1)
		// no return here, to avoid repeating the logCtx.inherit
	}

	// t1: --O--O------
	// t2: ------O--O--
	t2[len(t2)-1].LogCtx.Inherit(t1[len(t1)-1].LogCtx)
	return append(t1, t2...)
}

func getfirsttime(l LocalTimeline) time.Time {
	for _, event := range l {
		if event.Date != nil && (event.LogCtx.FileType == "error.log" || event.LogCtx.FileType == "") {
			return event.Date.Time
		}
	}
	return time.Time{}
}
func getlasttime(l LocalTimeline) time.Time {
	for i := len(l) - 1; i >= 0; i-- {
		if l[i].Date != nil && (l[i].LogCtx.FileType == "error.log" || l[i].LogCtx.FileType == "") {
			return l[i].Date.Time
		}
	}
	return time.Time{}
}

// CutTimelineAt returns a localtimeline with the 1st event starting
// right after the time sent as parameter
func CutTimelineAt(t LocalTimeline, at time.Time) LocalTimeline {
	var i int
	for i = 0; i < len(t); i++ {
		if t[i].Date != nil && t[i].Date.Time.After(at) {
			break
		}
	}

	return t[i:]
}

func (t *Timeline) GetLatestContextsByNodes() map[string]LogCtx {
	latestlogCtxs := make(map[string]LogCtx, len(*t))

	for key, localtimeline := range *t {
		latestlogCtxs[key] = localtimeline[len(localtimeline)-1].LogCtx
	}

	return latestlogCtxs
}

// iterateNode is used to search the source node(s) that contains the next chronological events
// it returns a slice in case 2 nodes have their next event precisely at the same time, which
// happens a lot on some versions
func (t Timeline) IterateNode() []string {
	var (
		nextDate  time.Time
		nextNodes []string
	)
	nextDate = time.Unix(math.MaxInt32, 0)
	for node := range t {
		if len(t[node]) == 0 {
			continue
		}
		curDate := getfirsttime(t[node])
		if curDate.Before(nextDate) {
			nextDate = curDate
			nextNodes = []string{node}
		} else if curDate.Equal(nextDate) {
			nextNodes = append(nextNodes, node)
		}
	}
	return nextNodes
}

func (t Timeline) Dequeue(node string) {

	// dequeue the events
	if len(t[node]) > 0 {
		t[node] = t[node][1:]
	}
}
