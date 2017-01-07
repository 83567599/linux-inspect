package psn

import (
	"bytes"
	"fmt"
	"log"
	"os/user"
	"sync"

	"github.com/gyuho/dataframe"
	"github.com/olekukonko/tablewriter"
)

// SSEntry is a socket entry.
// Simplied from 'NetTCP'.
type SSEntry struct {
	Protocol string

	Program string
	State   string
	PID     int64

	LocalIP   string
	LocalPort int64

	RemoteIP   string
	RemotePort int64

	User user.User
}

// GetSS finds all SSEntry by given filter.
func GetSS(opts ...FilterFunc) (sss []SSEntry, err error) {
	ft := &EntryFilter{}
	ft.applyOpts(opts)

	var pids []int64
	switch {
	case ft.ProgramMatchFunc == nil && ft.PID < 1:
		// get all PIDs
		pids, err = ListPIDs()
		if err != nil {
			return
		}

	case ft.PID > 0:
		pids = []int64{ft.PID}

	case ft.ProgramMatchFunc != nil:
		// later to find PIDs by Program
		pids = nil

	default:
		// applyOpts already panic when ft.ProgramMatchFunc != nil && ft.PID > 0
	}

	var pmu sync.RWMutex
	var wg sync.WaitGroup
	if len(pids) > 0 {
		// we already know PIDs to query

		wg.Add(len(pids))
		if ft.TCP && ft.TCP6 {
			wg.Add(len(pids))
		}
		for _, pid := range pids {
			if ft.TCP {
				go func(pid int64) {
					defer wg.Done()

					ents, err := getSSEntry(pid, TypeTCP, ft.LocalPort, ft.RemotePort)
					if err != nil {
						log.Printf("getSSEntry error %v for PID %d", err, pid)
						return
					}

					pmu.RLock()
					done := ft.TopLimit > 0 && len(sss) >= ft.TopLimit
					pmu.RUnlock()
					if done {
						return
					}

					pmu.Lock()
					sss = append(sss, ents...)
					pmu.Unlock()
				}(pid)
			}
			if ft.TCP6 {
				go func(pid int64) {
					defer wg.Done()

					ents, err := getSSEntry(pid, TypeTCP6, ft.LocalPort, ft.RemotePort)
					if err != nil {
						log.Printf("getSSEntry error %v for PID %d", err, pid)
						return
					}

					pmu.RLock()
					done := ft.TopLimit > 0 && len(sss) >= ft.TopLimit
					pmu.RUnlock()
					if done {
						return
					}

					pmu.Lock()
					sss = append(sss, ents...)
					pmu.Unlock()
				}(pid)
			}
		}
	} else {
		// find PIDs by Program
		pids, err = ListPIDs()
		if err != nil {
			return
		}

		up, err := GetUptime()
		if err != nil {
			return nil, err
		}
		wg.Add(len(pids))
		if ft.TCP && ft.TCP6 {
			wg.Add(len(pids))
		}
		for _, pid := range pids {
			if ft.TCP {
				go func(pid int64) {
					defer wg.Done()

					stat, err := GetStat(pid, up)
					if err != nil {
						log.Printf("GetStat error %v for PID %d", err, pid)
						return
					}
					if !ft.ProgramMatchFunc(stat.Comm) {
						return
					}

					pmu.RLock()
					done := ft.TopLimit > 0 && len(sss) >= ft.TopLimit
					pmu.RUnlock()
					if done {
						return
					}

					ents, err := getSSEntry(pid, TypeTCP, ft.LocalPort, ft.RemotePort)
					if err != nil {
						log.Printf("getSSEntry error %v for PID %d", err, pid)
						return
					}

					pmu.Lock()
					sss = append(sss, ents...)
					pmu.Unlock()
				}(pid)
			}
			if ft.TCP6 {
				go func(pid int64) {
					defer wg.Done()

					stat, err := GetStat(pid, up)
					if err != nil {
						log.Printf("GetStat error %v for PID %d", err, pid)
						return
					}
					if !ft.ProgramMatchFunc(stat.Comm) {
						return
					}

					pmu.RLock()
					done := ft.TopLimit > 0 && len(sss) >= ft.TopLimit
					pmu.RUnlock()
					if done {
						return
					}

					ents, err := getSSEntry(pid, TypeTCP6, ft.LocalPort, ft.RemotePort)
					if err != nil {
						log.Printf("getSSEntry error %v for PID %d", err, pid)
						return
					}

					pmu.Lock()
					sss = append(sss, ents...)
					pmu.Unlock()
				}(pid)
			}
		}
	}
	wg.Wait()

	if ft.TopLimit > 0 && len(sss) > ft.TopLimit {
		sss = sss[:ft.TopLimit:ft.TopLimit]
	}
	return
}

func getSSEntry(pid int64, tp TransportProtocol, lport int64, rport int64) (sss []SSEntry, err error) {
	nss, nerr := GetNetTCP(pid, tp)
	if nerr != nil {
		return nil, nerr
	}
	pname, perr := GetProgram(pid)
	if perr != nil {
		return nil, perr
	}

	for _, elem := range nss {
		u, uerr := user.LookupId(fmt.Sprintf("%d", elem.Uid))
		if uerr != nil {
			return nil, uerr
		}
		if lport > 0 && lport != elem.LocalAddressParsedIPPort {
			continue
		}
		if rport > 0 && rport != elem.RemAddressParsedIPPort {
			continue
		}
		entry := SSEntry{
			Protocol: elem.Type,

			Program: pname,
			State:   elem.StParsedStatus,
			PID:     pid,

			LocalIP:   elem.LocalAddressParsedIPHost,
			LocalPort: elem.LocalAddressParsedIPPort,

			RemoteIP:   elem.RemAddressParsedIPHost,
			RemotePort: elem.RemAddressParsedIPPort,

			User: *u,
		}
		sss = append(sss, entry)
	}

	return
}

const columnsSSToShow = 9

var columnsSSEntry = []string{
	"PROTOCOL",

	"PROGRAM",
	"STATE",
	"PID",

	"LOCAL-IP",
	"LOCAL-PORT",

	"REMOTE-IP",
	"REMOTE-PORT",

	"USER",
}

// ConvertSS converts to rows.
func ConvertSS(nss ...SSEntry) (header []string, rows [][]string) {
	header = columnsSSEntry
	rows = make([][]string, len(nss))
	for i, elem := range nss {
		row := make([]string, len(columnsSSEntry))
		row[0] = elem.Protocol

		row[1] = elem.Program
		row[2] = elem.State
		row[3] = fmt.Sprintf("%d", elem.PID)

		row[4] = elem.LocalIP
		row[5] = fmt.Sprintf("%d", elem.LocalPort)

		row[6] = elem.RemoteIP
		row[7] = fmt.Sprintf("%d", elem.RemotePort)

		row[8] = elem.User.Username

		rows[i] = row
	}
	dataframe.SortBy(
		rows,
		dataframe.StringAscendingFunc(1), // Program
		dataframe.StringAscendingFunc(2), // State
		dataframe.StringAscendingFunc(0), // Protocol
		dataframe.StringAscendingFunc(3), // PID
		dataframe.StringAscendingFunc(4), // LocalIP
	).Sort(rows)

	return
}

// StringSS converts in print-friendly format.
func StringSS(header []string, rows [][]string, topLimit int) string {
	buf := new(bytes.Buffer)
	tw := tablewriter.NewWriter(buf)
	tw.SetHeader(header[:columnsSSToShow:columnsSSToShow])

	if topLimit > 0 && len(rows) > topLimit {
		rows = rows[:topLimit:topLimit]
	}

	for _, row := range rows {
		tw.Append(row[:columnsSSToShow:columnsSSToShow])
	}
	tw.Render()

	return buf.String()
}
