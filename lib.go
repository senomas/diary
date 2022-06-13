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
	"strings"
	"time"
)

var dpattern = regexp.MustCompile(`^(\d\d\d\d)/(\d\d)/(\d\d\d\d)-(\d\d)-(\d\d)\.md$`)
var mdTimePattern = regexp.MustCompile(`^##\s+(\d\d:\d\d:\d\d)\s*$`)

type Journal struct {
	path   string
	Editor string
	Doings map[string][]Tag
	Todos  map[string][]Tag
	Laters map[string][]Tag
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
	Text   string
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

func (j *Journal) Push() {
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
		cmd = exec.Command("git", "-C", j.path, "commit", "-m", time.Now().Format("2006-01-02 15:04:05"))
		cmd.Stdin = os.Stdin
		cmd.Stdout = os.Stdout
		cmd.Stderr = os.Stderr
		err = cmd.Run()
		if err != nil {
			panic(fmt.Sprintf("error run git add %#v\n", err))
		}
	}
	cmd = exec.Command("git", "-C", j.path, "pull", "--rebase")
	err = cmd.Run()
	if err != nil {
		panic(fmt.Sprintf("error run git push %#v\n", err))
	}
	cmd = exec.Command("git", "-C", j.path, "push")
	err = cmd.Run()
	if err != nil {
		panic(fmt.Sprintf("error run git push %#v\n", err))
	}
}

func (j *Journal) Write() {
	data, err := json.MarshalIndent(j, "", "  ")
	if err != nil {
		panic(fmt.Sprintf("error marshal json %+v", err))
	}
	err = ioutil.WriteFile(filepath.Join(j.path, ".journal.json"), data, 0644)
	if err != nil {
		panic(fmt.Sprintf("error write file %+v", err))
	}
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

	cmd := exec.Command("git", "-C", j.path, "add", ".")
	cmd.Stdin = os.Stdin
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	err = cmd.Run()
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

func (j *Journal) writeTags(out *os.File, tagMap map[string][]Tag) {
	var tags []Tag
	for _, n := range tagMap {
		for _, t := range n {
			tags = append(tags, t)
		}
	}
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
	cmd = exec.Command("git", "-C", j.path, "status", "--porcelain")
	out = bytes.Buffer{}
	cmd.Stdout = &out
	err = cmd.Run()
	if err != nil {
		panic(fmt.Sprintf("error run git status %+v\n", err))
	}
	for _, fn := range strings.Split(out.String(), "\n") {
		if strings.HasSuffix(fn, ".md") {
			fn = strings.TrimSpace(fn)
			fn = strings.TrimSpace(fn[strings.IndexAny(fn, " "):])
			if _, ok := changes[fn]; !ok {
				changes[fn] = j.NewNote(fn)
			}
		}
	}
	for _, v := range changes {
		v.process()
	}
}

func (j *Journal) processAll() {
	pl := len(j.path)
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
	fin, err := os.Open(filepath.Join(n.journal.path, n.Path))
	if err != nil {
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
				texts = append(texts, fmt.Sprintf("*[DOING](%s)*", n.Path))
			case "*TODO*":
				todo = true
				texts = append(texts, fmt.Sprintf("*[TODO](%s)*", n.Path))
			case "*LATER*":
				later = true
				texts = append(texts, fmt.Sprintf("*[LATER](%s)*", n.Path))
			default:
				texts = append(texts, w)
			}
		}
		if doing {
			doings = append(doings, Tag{note: n, Time: ctime, LineNo: lineNo, Text: strings.Join(texts, " ")})
		}
		if todo {
			todos = append(todos, Tag{note: n, Time: ctime, LineNo: lineNo, Text: strings.Join(texts, " ")})

		}
		if later {
			laters = append(laters, Tag{note: n, Time: ctime, LineNo: lineNo, Text: strings.Join(texts, " ")})
		}
		lineNo++
	}
	n.journal.Doings[n.Path] = doings
	n.journal.Todos[n.Path] = todos
	n.journal.Laters[n.Path] = laters
}
