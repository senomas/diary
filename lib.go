package main

import (
	"bufio"
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"io/ioutil"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"
)

var dpattern = regexp.MustCompile(`^(\d\d\d\d)/(\d\d)/(\d\d\d\d)-(\d\d)-(\d\d)\.md$`)
var mdTimePattern = regexp.MustCompile(`^##\s+(\d\d:\d\d:\d\d)\s*$`)
var headerPattern = regexp.MustCompile(`^#+\s+(.*)`)

type Journal struct {
	path   string
	Hash   string
	Editor string
	Doings map[string][]Tag
	Todos  map[string][]Tag
	Laters map[string][]Tag
	Diary  map[string][][]string
}

type NoteType int8

const (
	NoteText NoteType = iota
	Diary
)

type Note struct {
	journal *Journal
	Path    string
	Type    NoteType
	Time    time.Time
}

type Tag struct {
	note   *Note
	Time   time.Time
	LineNo int
	Tag    string
	Text   string
}

type TagCount struct {
	Tag   string
	Count int
}

type TagCounts []TagCount

func (tc TagCounts) Len() int {
	return len(tc)
}

func (tc TagCounts) Less(i, j int) bool {
	return tc[i].Count < tc[j].Count
}

func (tc TagCounts) Swap(i, j int) {
	tc[i], tc[j] = tc[j], tc[i]
}

func OpenJournal(path string) *Journal {
	journal := Journal{path: path, Editor: "lvim", Doings: make(map[string][]Tag), Todos: make(map[string][]Tag), Laters: make(map[string][]Tag)}
	file, err := os.Open(filepath.Join(path, ".journal.json"))
	if err != nil {
		if !errors.Is(err, os.ErrNotExist) {
			panic(fmt.Sprintf("error open config file %+v", err))
		}
	} else {
		defer file.Close()
		data, err := ioutil.ReadAll(file)
		if err != nil {
			panic(fmt.Sprintf("error open config file %+v", err))
		}
		json.Unmarshal(data, &journal)
	}
	cmd := exec.Command("git", "-C", path, "push")
	err = cmd.Start()
	if err != nil {
		panic(fmt.Sprintf("error run git push %#v\n", err))
	}
	return &journal
}

func (j *Journal) Commit() {
	cmd := exec.Command("git", "-C", j.path, "add", ".")
	cmd.Stdin = os.Stdin
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	err := cmd.Run()
	if err != nil {
		panic(fmt.Sprintf("error run git add %#v\n", err))
	}
	cmd = exec.Command("git", "-C", j.path, "status", "--porcelain")
	var out bytes.Buffer
	cmd.Stdout = &out
	err = cmd.Run()
	if err != nil {
		panic(fmt.Sprintf("error run git status %#v\n", err))
	}
	if strings.TrimSpace(out.String()) != "" {
		cmd = exec.Command("git", "-C", j.path, "rev-parse", "HEAD")
		var out bytes.Buffer
		cmd.Stdout = &out
		err := cmd.Run()
		if err != nil {
			panic(fmt.Sprintf("error run git status %+v\n", err))
		}
		j.Hash = strings.TrimSpace(out.String())
		j.writeConfig()

		cmd = exec.Command("git", "-C", j.path, "add", ".")
		cmd.Stdin = os.Stdin
		cmd.Stdout = os.Stdout
		cmd.Stderr = os.Stderr
		err = cmd.Run()
		if err != nil {
			panic(fmt.Sprintf("error run git add %#v\n", err))
		}
		cmd = exec.Command("git", "-C", j.path, "commit", "-m", time.Now().Format("2006-01-02 15:04:05"))
		cmd.Stdin = os.Stdin
		cmd.Stdout = os.Stdout
		cmd.Stderr = os.Stderr
		err = cmd.Run()
		if err != nil {
			panic(fmt.Sprintf("error run git add %#v\n", err))
		}
	}
}

func (j *Journal) Push() {
	j.Commit()
	cmd := exec.Command("git", "-C", j.path, "pull", "--rebase")
	err := cmd.Run()
	if err != nil {
		panic(fmt.Sprintf("error run git push %#v\n", err))
	}
	cmd = exec.Command("git", "-C", j.path, "push")
	err = cmd.Run()
	if err != nil {
		panic(fmt.Sprintf("error run git push %#v\n", err))
	}
}

func (j *Journal) writeConfig() {
	data, err := json.MarshalIndent(j, "", "  ")
	if err != nil {
		panic(fmt.Sprintf("error marshal json %+v", err))
	}
	err = ioutil.WriteFile(filepath.Join(j.path, ".journal.json"), data, 0644)
	if err != nil {
		panic(fmt.Sprintf("error write file %+v", err))
	}
}

func (j *Journal) Write() {
	j.writeConfig()
	fout, err := os.Create(filepath.Join(j.path, "index.md"))
	if err != nil {
		panic(fmt.Sprintf("error write index file %+v", err))
	}
	defer fout.Close()
	fout.WriteString("# DOING\n\n")
	j.writeTags(fout, j.Doings)

	fout.WriteString("\n# TODO\n\n")
	j.writeTags(fout, j.Todos)

	fout.WriteString("\n# LATER\n\n")
	j.writeTags(fout, j.Laters)

	fout.Close()
	j.Commit()
}

func (j *Journal) writeTags(out *os.File, tagMap map[string][]Tag) {
	var tags []Tag
	for _, n := range tagMap {
		for _, t := range n {
			tags = append(tags, t)
		}
	}
	sort.Slice(tags, func(i, j int) bool {
		return tags[i].Time.After(tags[j].Time)
	})
	for _, t := range tags {
		out.WriteString(fmt.Sprintf("%s\n", t.Text))
	}
}

func (j *Journal) OpenIndex() {
	j.processChanges()
	cmd := exec.Command(j.Editor, filepath.Join(j.path, "index.md"))
	cmd.Stdin = os.Stdin
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	err := cmd.Run()
	if err != nil {
		panic(fmt.Sprintf("error run %#v\n", err))
	}
	j.processChanges()
	j.Write()
}

func (j *Journal) CreateDiary() {
	now := time.Now()
	fp := now.Format("2006/01")
	err := os.MkdirAll(filepath.Join(j.path, fp), os.ModePerm)
	if err != nil {
		panic(fmt.Sprintf("error create path '%s' %+v\n", fp, err))
	}
	fn := now.Format("2006-01-02.md")
	ff := filepath.Join(j.path, fp, fn)
	if _, err := os.Stat(ff); err == nil {
		cmd := exec.Command(j.Editor,
			"-c", "norm Go",
			"-c", fmt.Sprintf("norm Go## %s", now.Format("15:04:05")),
			"-c", "norm G2o",
			"-c", "norm zz",
			"-c", "startinsert", ff,
		)
		cmd.Stdin = os.Stdin
		cmd.Stdout = os.Stdout
		cmd.Stderr = os.Stderr
		err = cmd.Run()
		if err != nil {
			panic(fmt.Sprintf("error run %#v\n", err))
		}
	} else if errors.Is(err, os.ErrNotExist) {
		cmd := exec.Command(j.Editor,
			"-c", fmt.Sprintf("norm Gi# Note %s", now.Format("2006-01-02")),
			"-c", "norm Go",
			"-c", fmt.Sprintf("norm Go## %s", now.Format("15:04:05")),
			"-c", "norm G2o",
			"-c", "norm zz",
			"-c", "startinsert", ff,
		)
		cmd.Stdin = os.Stdin
		cmd.Stdout = os.Stdout
		cmd.Stderr = os.Stderr
		err = cmd.Run()
		if err != nil {
			panic(fmt.Sprintf("error run %#v\n", err))
		}
	} else {
		panic(fmt.Sprintf("error create file '%s' %#v\n", ff, err))
	}
	j.processChanges()
	j.Write()
}

func (j *Journal) NewNote(fn string) *Note {
	pfn := filepath.Join(j.path, fn)
	if st, err := os.Stat(pfn); err != nil {
		panic(fmt.Sprintf("Error read file '%s' %+v\n", fn, err))
	} else {
		if ms := dpattern.FindAllStringSubmatch(fn, -1); ms != nil && len(ms) == 1 && len(ms[0]) == 6 && ms[0][1] == ms[0][3] && ms[0][2] == ms[0][4] {
			dt := fmt.Sprintf("%s-%s-%sT00:00:00", ms[0][3], ms[0][4], ms[0][5])
			time, err := time.ParseInLocation("2006-01-02T15:04:05", dt, time.Local)
			if err != nil {
				panic(fmt.Sprintf("error date format '%s' %+v", dt, err))
			}
			return &Note{
				journal: j,
				Path:    fn,
				Type:    Diary,
				Time:    time,
			}
		} else {
			return &Note{
				journal: j,
				Path:    fn,
				Type:    NoteText,
				Time:    st.ModTime(),
			}
		}
	}
}

func (j *Journal) processChanges() {
	if j.Hash == "" {
		j.processAll()
		return
	}
	cmd := exec.Command("git", "-C", j.path, "ls-files", ".", "--exclude-standard", "--others")
	var out bytes.Buffer
	cmd.Stdout = &out
	err := cmd.Run()
	if err != nil {
		panic(fmt.Sprintf("error run git ls-files %+v\n", err))
	}
	changes := make(map[string]*Note)
	for _, fn := range strings.Split(out.String(), "\n") {
		if strings.HasSuffix(fn, ".md") {
			if _, ok := changes[fn]; !ok {
				changes[fn] = j.NewNote(fn)
			}
		}
	}
	cmd = exec.Command("git", "-C", j.path, "diff", j.Hash, "--name-only")
	out = bytes.Buffer{}
	cmd.Stdout = &out
	err = cmd.Run()
	if err != nil {
		panic(fmt.Sprintf("error run git status %+v\n", err))
	}
	for _, fn := range strings.Split(out.String(), "\n") {
		if strings.HasSuffix(fn, ".md") {
			ff := filepath.Join(j.path, fn)
			if _, err := os.Stat(ff); err == nil {
				if _, ok := changes[fn]; !ok {
					changes[fn] = j.NewNote(fn)
				}
			}
		}
	}
	for _, v := range changes {
		v.process()
	}
}

func (j *Journal) processAll() {
	pl := len(j.path)
	j.Doings = make(map[string][]Tag)
	j.Todos = make(map[string][]Tag)
	j.Laters = make(map[string][]Tag)
	j.Diary = make(map[string][][]string)
	filepath.WalkDir(j.path, func(path string, d fs.DirEntry, err error) error {
		if d.Name() == ".git" {
			return filepath.SkipDir
		}
		if d.Name() == "index.md" {
			// ignore
		} else if strings.HasSuffix(path, ".md") {
			fn := path[pl:]
			n := j.NewNote(fn)
			n.process()
		}
		return nil
	})
}

func (n *Note) process() {
	if strings.HasSuffix(n.Path, "/index.md") || n.Path == "index.md" {
		return
	}
	fin, err := os.Open(filepath.Join(n.journal.path, n.Path))
	if err != nil {
		if !errors.Is(err, os.ErrNotExist) {
			delete(n.journal.Doings, n.Path)
			delete(n.journal.Todos, n.Path)
			delete(n.journal.Laters, n.Path)
			return
		}
		panic(fmt.Sprintf("error processing '%s' %+v\n", n.Path, err))
	}
	defer fin.Close()
	scanner := bufio.NewScanner(fin)
	var nd = n.Time.Format("2006-01-02")
	var nt = n.Time.Format("15:04:05")
	var ctime = n.Time
	lineNo := 1
	var doings []Tag
	var todos []Tag
	var laters []Tag
	for scanner.Scan() {
		text := scanner.Text()
		if ms := mdTimePattern.FindAllStringSubmatch(text, -1); ms != nil {
			nt = ms[0][1]
			ctime, err = time.ParseInLocation("2006-01-02T15:04:05", fmt.Sprintf("%sT%s", nd, nt), time.Local)
			if err != nil {
				panic(fmt.Sprintf("error parse date '%sT%s' %+v\n", nd, nt, err))
			}
		}
		var doing = false
		var todo = false
		var later = false
		var texts []string
		for _, w := range strings.Fields(text) {
			switch w {
			case "*DOING*":
				doing = true
				texts = append(texts, fmt.Sprintf("*[DOING](%s#%s)*", n.Path, nt))
			case "*TODO*":
				todo = true
				texts = append(texts, fmt.Sprintf("*[TODO](%s#%s)*", n.Path, nt))
			case "*LATER*":
				later = true
				texts = append(texts, fmt.Sprintf("*[LATER](%s#%s)*", n.Path, nt))
			default:
				texts = append(texts, w)
			}
		}
		ftext := strings.Join(texts, " ")
		if doing {
			doings = append(doings, Tag{note: n, Time: ctime, LineNo: lineNo, Text: ftext})
		}
		if todo {
			todos = append(todos, Tag{note: n, Time: ctime, LineNo: lineNo, Text: ftext})
		}
		if later {
			laters = append(laters, Tag{note: n, Time: ctime, LineNo: lineNo, Text: ftext})
		}
		lineNo++
	}
	if len(doings) > 0 {
		n.journal.Doings[n.Path] = doings
	} else {
		delete(n.journal.Doings, n.Path)
	}
	if len(todos) > 0 {
		n.journal.Todos[n.Path] = todos
	} else {
		delete(n.journal.Todos, n.Path)
	}
	if len(laters) > 0 {
		n.journal.Laters[n.Path] = laters
	} else {
		delete(n.journal.Laters, n.Path)
	}
	now := time.Now()
	lastYearMonth := now.Year()*12 + int(now.Month()) - 3
	if ms := dpattern.FindAllStringSubmatch(n.Path, -1); ms != nil && len(ms) == 1 && len(ms[0]) == 6 && ms[0][1] == ms[0][3] && ms[0][2] == ms[0][4] {
		dt := fmt.Sprintf("%s-%s-%sT00:00:00", ms[0][3], ms[0][4], ms[0][5])
		dtime, err := time.ParseInLocation("2006-01-02T15:04:05", dt, time.Local)
		if err != nil {
			panic(fmt.Sprintf("error date format '%s' %+v", dt, err))
		}
		yearMonth := dtime.Year()*12 + int(dtime.Month()) - 1
		delta := yearMonth - lastYearMonth
		if delta > 0 {
			dtg := fmt.Sprintf("%s-%s", ms[0][3], ms[0][4])
			n.journal.Diary[dtg] = append(n.journal.Diary[dtg], []string{ms[0][5], n.Path})
		}
	}
}
