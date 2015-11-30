package main

import (
	"bufio"
	"bytes"
	"compress/gzip"
	"crypto/tls"
	"encoding/gob"
	"flag"
	"fmt"
	"net/http"
	"os"
	"sort"
	"strings"
	"sync"

	"bitbucket.org/snapbug/hsr/common/regexp"
)

var (
	gameVersion      = regexp.New(`gameVersion = (?P<version>\d+)`)
	screenTransition = regexp.New(`OnSceneLoaded\(\) - prevMode=(?P<prev>\S+) currMode=(?P<curr>\S+)`)
	gameServer       = regexp.New(`GotoGameServer -- address=(?P<ip>.+):(?P<port>\d+), game=(?P<game>\d+), client=(?P<client>\d+), spectateKey=(?P<key>.+)`)
)

type FileAndLine struct {
	ts  string
	scn *bufio.Scanner
	fn  string
}

func (fl *FileAndLine) Update() bool {
	if fl.scn.Scan() {
		old_text := fl.ts
		fl.ts = strings.Split(fl.scn.Text(), " ")[1]

		if fl.ts < old_text {
			fl.ts = "E" + fl.ts // dirty cheat to get around the time stuff!
		}
		return true
	}
	return false
}

type FileAndLines []*FileAndLine

func (fl FileAndLines) Len() int           { return len(fl) }
func (fl FileAndLines) Swap(i, j int)      { fl[i], fl[j] = fl[j], fl[i] }
func (fl FileAndLines) Less(i, j int) bool { return strings.Compare(fl[i].ts, fl[j].ts) < 0 }

type Log struct {
	Type     string
	Version  string
	Uploader string
	Key      string
	Data     []byte

	data bytes.Buffer
}

const (
	url = "https://hearthreplay.com"
)

var (
	tr = &http.Transport{
		TLSClientConfig: &tls.Config{InsecureSkipVerify: true},
	}
	client = &http.Client{Transport: tr}
)

func upload(l Log, wg *sync.WaitGroup) {
	defer wg.Done()

	path := fmt.Sprintf("%s/g/%s/%s/", url, l.Uploader, l.Key)
	resp, err := client.Head(path)

	if err != nil {
		fmt.Printf("head failed: %#v\n", err)
	} else if resp.StatusCode == http.StatusOK {
		fmt.Printf("Already uploaded %s/%s -- skipping\n", l.Uploader, l.Key)
		return
	} else {
		fmt.Printf("head failed: %#v\n", resp)
	}

	var y bytes.Buffer

	gz := gzip.NewWriter(&y)
	gz.Write(l.data.Bytes())
	gz.Close()
	l.Data = y.Bytes()

	var x bytes.Buffer
	enc := gob.NewEncoder(&x)
	err = enc.Encode(l)

	if err != nil {
		panic(err)
	}

	resp, err = client.Post(path, "appliation/octet-stream", &x)

	if err != nil {
		fmt.Printf("Error contacting server: %s\n", err)
	} else if resp.StatusCode != http.StatusAccepted {
		fmt.Printf("Server returned: %s -- report %s/%s", l.Uploader, l.Key)
	} else {
		fmt.Printf("Uploaded %s game: %s/g/%s/%s\n", l.Type, url, l.Uploader, l.Key)
	}
}

var (
	debug string
)

func getLogs(filenames []string) chan Log {
	x := make(chan Log)
	go func(filenames []string) {
		var dbgout *os.File
		var err error

		var versionLine string
		var version string
		var gameType string
		var gameTypeLine string

		var log Log
		var found_log bool

		logsandlines := make(FileAndLines, 0)

		for _, fn := range filenames {
			f, err := os.Open(fn)
			if err != nil {
				panic(err)
			} else {
				fandl := &FileAndLine{scn: bufio.NewScanner(f), fn: fn}
				if fandl.Update() {
					logsandlines = append(logsandlines, fandl)
				}
			}
		}

		for logsandlines.Len() > 0 {
			sort.Sort(logsandlines)
			text := logsandlines[0].scn.Text()

			if gameVersion.MatchString(text) {
				p := gameVersion.NamedMatches(text)
				version = p["version"]
				versionLine = text
			}

			if screenTransition.MatchString(text) {
				trans := screenTransition.NamedMatches(text)
				gameTypeLine = text
				gameType = trans["curr"]
			}

			if gameServer.MatchString(text) {
				gs := gameServer.NamedMatches(text)
				fmt.Printf("%s Game: %s/%s/%s @ %s:%s\n", gameType, gs["game"], gs["client"], gs["key"], gs["ip"], gs["key"])

				if found_log {
					x <- log
				}
				log = Log{
					Type:     gameType,
					Uploader: gs["client"],
					Key:      fmt.Sprintf("%s-%s", gs["game"], gs["key"]),
					Version:  version,
				}
				found_log = true

				if debug != "" && dbgout != nil {
					if err = dbgout.Close(); err != nil {
						panic(err)
					}
				}
				if debug != "" {
					dbgout, err = os.Create(fmt.Sprintf("out/%s.%s.%s.(%s).log", gs["client"], gs["game"], gs["key"], gameType))
					if err != nil {
						panic(err)
					}
					fmt.Fprintf(dbgout, "%s\n", versionLine)
					fmt.Fprintf(dbgout, "%s\n", gameTypeLine)
					fmt.Fprintf(dbgout, "%s\n", text)
				}

				log.data.WriteString(fmt.Sprintf("%s\n", versionLine))
				log.data.WriteString(fmt.Sprintf("%s\n", gameTypeLine))
				log.data.WriteString(fmt.Sprintf("%s\n", text))
			} else {
				if strings.Contains(text, "GameState") {
					if debug != "" && dbgout != nil {
						fmt.Fprintf(dbgout, "%s\n", text)
					}
					log.data.WriteString(fmt.Sprintf("%s\n", text))
				}
			}

			if !logsandlines[0].Update() {
				logsandlines = logsandlines[1:]
			}
		}

		if found_log {
			x <- log
		}

		close(x)
	}(filenames)
	return x
}

func main() {
	var wg sync.WaitGroup
	flag.Parse()

	for log := range getLogs(flag.Args()) {
		wg.Add(1)
		if false {
			go upload(log, &wg)
		} else {
			wg.Done()
		}
	}
	wg.Wait()
	fmt.Println("Fin!")
}
