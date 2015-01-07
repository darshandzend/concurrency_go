package main

import (
	"bufio"
	"bytes"
	"fmt"
	"io"
	"log"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
)

var workers = runtime.NumCPU()

type Result struct {
	filename string
	lino     int
	line     string
}

type Job struct {
	filename string
	results  chan<- Result
}

func (job Job) Do(lineRx *regexp.Regexp) {
	file, err := os.Open(job.filename)
	if err != nil {
		log.Printf("error: %s\n", err)
		return
	}
	defer file.Close()
	reader := bufio.NewReader(file)
	for lino := 1; ; lino++ {
		line, err := reader.ReadBytes('\n')
		line = bytes.TrimRight(line, "\n\r")
		if lineRx.Match(line) {
			job.results <- Result{job.filename, lino, string(line)}
		}
		if err != nil {
			if err != io.EOF {
				log.Printf("error:%d: %s\n", lino, err)
			}
			break
		}
	}
}

func main() {
	runtime.GOMAXPROCS(runtime.NumCPU()) // Use all the machine's cores
	if len(os.Args) < 3 || os.Args[1] == "-h" || os.Args[1] == "--help" {
		fmt.Printf("usage: %s <regexp> <files>\n",
			filepath.Base(os.Args[0]))
		os.Exit(1)
	}
	if lineRx, err := regexp.Compile(os.Args[1]); err != nil {
		log.Fatalf("invalid regexp: %s\n", err)
	} else {
		grep(lineRx, commandLineFiles(os.Args[2:]))
	}
}

func commandLineFiles(files []string) []string {
	if runtime.GOOS == "windows" {
		args := make([]string, 0, len(files))
		for _, name := range files {
			if matches, err := filepath.Glob(name); err != nil {
				args = append(args, name) // Invalid pattern
			} else if matches != nil { // At least one match
				args = append(args, matches...)
			}
		}
		return args
	}
	return files
}

func grep(lineRx *regexp.Regexp, filenames []string) {
	jobs := make(chan Job, workers)
	results := make(chan Result, minimum(1000, len(filenames)))
	done := make(chan struct{}, workers)

	go addJobs(jobs, filenames, results) // Executes in its own goroutine
	for i := 0; i < workers; i++ {
		go doJobs(done, lineRx, jobs) // Each executes in its own goroutine
	}
	go awaitCompletion(done, results) // Executes in its own goroutine
	processResults(results)           // Blocks until the work is done
}

func addJobs(jobs chan<- Job, filenames []string, results chan<- Result) {
	for _, filename := range filenames {
		jobs <- Job{filename, results}
	}
	close(jobs)
}

func doJobs(done chan<- struct{}, lineRx *regexp.Regexp, jobs <-chan Job) {
	for job := range jobs {
		job.Do(lineRx)
	}
	done <- struct{}{}
}

func awaitCompletion(done <-chan struct{}, results chan Result) {
	for i := 0; i < workers; i++ {
		<-done
	}
	close(results)
}

func processResults(results <-chan Result) {
	for result := range results {
		fmt.Printf("%s:%d:%s\n", result.filename, result.lino, result.line)
	}
}

func minimum(x int, ys ...int) int {
	for _, y := range ys {
		if y < x {
			x = y
		}
	}
	return x
}
package main

import (
	"flag"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"runtime"
	"strings"
)

func main() {
	runtime.GOMAXPROCS(runtime.NumCPU()) // Use all the machine's cores
	log.SetFlags(0)
	algorithm,
		minSize, maxSize, suffixes, files := handleCommandLine()

	if algorithm == 1 {
		sink(filterSize(minSize, maxSize, filterSuffixes(suffixes, source(files))))
	} else {
		channel1 := source(files)
		channel2 := filterSuffixes(suffixes, channel1)
		channel3 := filterSize(minSize, maxSize, channel2)
		sink(channel3)
	}
}

func handleCommandLine() (algorithm int, minSize, maxSize int64,
	suffixes, files []string) {
	flag.IntVar(&algorithm, "algorithm", 1, "1 or 2")
	flag.Int64Var(&minSize, "min", -1,
		"minimum file size (-1 means no minimum)")
	flag.Int64Var(&maxSize, "max", -1,
		"maximum file size (-1 means no maximum)")
	var suffixesOpt *string = flag.String("suffixes", "",
		"comma-separated list of file suffixes")
	flag.Parse()
	if algorithm != 1 && algorithm != 2 {
		algorithm = 1
	}
	if minSize > maxSize && maxSize != -1 {
		log.Fatalln("minimum size must be < maximum size")
	}
	suffixes = []string{}
	if *suffixesOpt != "" {
		suffixes = strings.Split(*suffixesOpt, ",")
	}
	files = flag.Args()
	return algorithm, minSize, maxSize, suffixes, files
}

func source(files []string) <-chan string {
	out := make(chan string, 1000)
	go func() {
		for _, filename := range files {
			out <- filename
		}
		close(out)
	}()
	return out
}

func filterSuffixes(suffixes []string, in <-chan string) <-chan string {
	out := make(chan string, cap(in))
	go func() {
		for filename := range in {
			if len(suffixes) == 0 {
				out <- filename
				continue
			}
			ext := strings.ToLower(filepath.Ext(filename))
			for _, suffix := range suffixes {
				if ext == suffix {
					out <- filename
					break
				}
			}
		}
		close(out)
	}()
	return out
}

func filterSize(minimum, maximum int64, in <-chan string) <-chan string {
	out := make(chan string, cap(in))
	go func() {
		for filename := range in {
			if minimum == -1 && maximum == -1 {
				out <- filename // don't do a stat call it not needed
				continue
			}
			finfo, err := os.Stat(filename)
			if err != nil {
				continue // ignore files we can't process
			}
			size := finfo.Size()
			if (minimum == -1 || minimum > -1 && minimum <= size) &&
				(maximum == -1 || maximum > -1 && maximum >= size) {
				out <- filename
			}
		}
		close(out)
	}()
	return out
}

func sink(in <-chan string) {
	for filename := range in {
		fmt.Println(filename)
	}
}
package main

import (
	"crypto/sha1"
	"fmt"
	"io"
	"log"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"sync"
)

const maxSizeOfSmallFile = 1024 * 32
const maxGoroutines = 100

type pathsInfo struct {
	size  int64
	paths []string
}

type fileInfo struct {
	sha1 []byte
	size int64
	path string
}

func main() {
	runtime.GOMAXPROCS(runtime.NumCPU()) // Use all the machine's cores
	if len(os.Args) == 1 || os.Args[1] == "-h" || os.Args[1] == "--help" {
		fmt.Printf("usage: %s <path>\n", filepath.Base(os.Args[0]))
		os.Exit(1)
	}

	infoChan := make(chan fileInfo, maxGoroutines*2)
	go findDuplicates(infoChan, os.Args[1])
	pathData := mergeResults(infoChan)
	outputResults(pathData)
}

func findDuplicates(infoChan chan fileInfo, dirname string) {
	waiter := &sync.WaitGroup{}
	filepath.Walk(dirname, makeWalkFunc(infoChan, waiter))
	waiter.Wait() // Blocks until all the work is done
	close(infoChan)
}

func makeWalkFunc(infoChan chan fileInfo,
	waiter *sync.WaitGroup) func(string, os.FileInfo, error) error {
	return func(path string, info os.FileInfo, err error) error {
		if err == nil && info.Size() > 0 &&
			(info.Mode()&os.ModeType == 0) {
			if info.Size() < maxSizeOfSmallFile ||
				runtime.NumGoroutine() > maxGoroutines {
				processFile(path, info, infoChan, nil)
			} else {
				waiter.Add(1)
				go processFile(path, info, infoChan,
					func() { waiter.Done() })
			}
		}
		return nil // We ignore all errors
	}
}

func processFile(filename string, info os.FileInfo,
	infoChan chan fileInfo, done func()) {
	if done != nil {
		defer done()
	}
	file, err := os.Open(filename)
	if err != nil {
		log.Println("error:", err)
		return
	}
	defer file.Close()
	hash := sha1.New()
	if size, err := io.Copy(hash, file); size != info.Size() || err != nil {
		if err != nil {
			log.Println("error:", err)
		} else {
			log.Println("error: failed to read the whole file:", filename)
		}
		return
	}
	infoChan <- fileInfo{hash.Sum(nil), info.Size(), filename}
}

func mergeResults(infoChan <-chan fileInfo) map[string]*pathsInfo {
	pathData := make(map[string]*pathsInfo)
	format := fmt.Sprintf("%%016X:%%%dX", sha1.Size*2) // == "%016X:%40X"
	for info := range infoChan {
		key := fmt.Sprintf(format, info.size, info.sha1)
		value, found := pathData[key]
		if !found {
			value = &pathsInfo{size: info.size}
			pathData[key] = value
		}
		value.paths = append(value.paths, info.path)
	}
	return pathData
}

func outputResults(pathData map[string]*pathsInfo) {
	keys := make([]string, 0, len(pathData))
	for key := range pathData {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	for _, key := range keys {
		value := pathData[key]
		if len(value.paths) > 1 {
			fmt.Printf("%d duplicate files (%s bytes):\n",
				len(value.paths), commas(value.size))
			sort.Strings(value.paths)
			for _, name := range value.paths {
				fmt.Printf("\t%s\n", name)
			}
		}
	}
}

func commas(x int64) string {
	value := fmt.Sprint(x)
	for i := len(value) - 3; i > 0; i -= 3 {
		value = value[:i] + "," + value[i:]
	}
	return value
}
package main

import (
	"bufio"
	"fmt"
	"io"
	"log"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
)

var workers = runtime.NumCPU()

func main() {
	runtime.GOMAXPROCS(runtime.NumCPU()) // Use all the machine's cores
	if len(os.Args) != 2 || os.Args[1] == "-h" || os.Args[1] == "--help" {
		fmt.Printf("usage: %s <file.log>\n", filepath.Base(os.Args[0]))
		os.Exit(1)
	}
	lines := make(chan string, workers*4)
	results := make(chan map[string]int, workers)
	go readLines(os.Args[1], lines)
	getRx := regexp.MustCompile(`GET[ \t]+([^ \t\n]+[.]html?)`)
	for i := 0; i < workers; i++ {
		go processLines(results, getRx, lines)
	}
	totalForPage := make(map[string]int)
	merge(results, totalForPage)
	showResults(totalForPage)
}

func readLines(filename string, lines chan<- string) {
	file, err := os.Open(filename)
	if err != nil {
		log.Fatal("failed to open the file:", err)
	}
	defer file.Close()
	reader := bufio.NewReader(file)
	for {
		line, err := reader.ReadString('\n')
		if line != "" {
			lines <- line
		}
		if err != nil {
			if err != io.EOF {
				log.Println("failed to finish reading the file:", err)
			}
			break
		}
	}
	close(lines)
}

func processLines(results chan<- map[string]int, getRx *regexp.Regexp,
	lines <-chan string) {
	countForPage := make(map[string]int)
	for line := range lines {
		if matches := getRx.FindStringSubmatch(line); matches != nil {
			countForPage[matches[1]]++
		}
	}
	results <- countForPage
}

func merge(results <-chan map[string]int, totalForPage map[string]int) {
	for i := 0; i < workers; i++ {
		countForPage := <-results
		for page, count := range countForPage {
			totalForPage[page] += count
		}
	}
}

func showResults(totalForPage map[string]int) {
	for page, count := range totalForPage {
		fmt.Printf("%8d %s\n", count, page)
	}
}
package main

import (
	"bufio"
	"fmt"
	"io"
	"log"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"sync"
)

var workers = runtime.NumCPU()

type pageMap struct {
	countForPage map[string]int
	mutex        *sync.RWMutex
}

func NewPageMap() *pageMap {
	return &pageMap{make(map[string]int), new(sync.RWMutex)}
}

func (pm *pageMap) Increment(page string) {
	pm.mutex.Lock()
	defer pm.mutex.Unlock()
	pm.countForPage[page]++
}

func (pm *pageMap) Len() int {
	pm.mutex.RLock()
	defer pm.mutex.RUnlock()
	return len(pm.countForPage)
}

func main() {
	runtime.GOMAXPROCS(runtime.NumCPU()) // Use all the machine's cores
	if len(os.Args) != 2 || os.Args[1] == "-h" || os.Args[1] == "--help" {
		fmt.Printf("usage: %s <file.log>\n", filepath.Base(os.Args[0]))
		os.Exit(1)
	}
	lines := make(chan string, workers*4)
	done := make(chan struct{}, workers)
	pageMap := NewPageMap()
	go readLines(os.Args[1], lines)
	getRx := regexp.MustCompile(`GET[ \t]+([^ \t\n]+[.]html?)`)
	for i := 0; i < workers; i++ {
		go processLines(done, getRx, pageMap, lines)
	}
	waitUntil(done)
	showResults(pageMap)
}

func readLines(filename string, lines chan<- string) {
	file, err := os.Open(filename)
	if err != nil {
		log.Fatal("failed to open the file:", err)
	}
	defer file.Close()
	reader := bufio.NewReader(file)
	for {
		line, err := reader.ReadString('\n')
		if line != "" {
			lines <- line
		}
		if err != nil {
			if err != io.EOF {
				log.Println("failed to finish reading the file:", err)
			}
			break
		}
	}
	close(lines)
}

func processLines(done chan<- struct{}, getRx *regexp.Regexp,
	pageMap *pageMap, lines <-chan string) {
	for line := range lines {
		if matches := getRx.FindStringSubmatch(line); matches != nil {
			pageMap.Increment(matches[1])
		}
	}
	done <- struct{}{}
}

func waitUntil(done <-chan struct{}) {
	for i := 0; i < workers; i++ {
		<-done
	}
}

func showResults(pageMap *pageMap) {
	// No lock, accesses in only one goroutine
	for page, count := range pageMap.countForPage {
		fmt.Printf("%8d %s\n", count, page)
	}
}
package main

import (
	"bufio"
	"fmt"
	"io"
	"log"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"safemap"
)

var workers = runtime.NumCPU()

func main() {
	runtime.GOMAXPROCS(runtime.NumCPU()) // Use all the machine's cores
	if len(os.Args) != 2 || os.Args[1] == "-h" || os.Args[1] == "--help" {
		fmt.Printf("usage: %s <file.log>\n", filepath.Base(os.Args[0]))
		os.Exit(1)
	}
	lines := make(chan string, workers*4)
	done := make(chan struct{}, workers)
	pageMap := safemap.New()
	go readLines(os.Args[1], lines)
	processLines(done, pageMap, lines)
	waitUntil(done)
	showResults(pageMap)
}

func readLines(filename string, lines chan<- string) {
	file, err := os.Open(filename)
	if err != nil {
		log.Fatal("failed to open the file:", err)
	}
	defer file.Close()
	reader := bufio.NewReader(file)
	for {
		line, err := reader.ReadString('\n')
		if line != "" {
			lines <- line
		}
		if err != nil {
			if err != io.EOF {
				log.Println("failed to finish reading the file:", err)
			}
			break
		}
	}
	close(lines)
}

func processLines(done chan<- struct{}, pageMap safemap.SafeMap,
	lines <-chan string) {
	getRx := regexp.MustCompile(`GET[ \t]+([^ \t\n]+[.]html?)`)
	incrementer := func(value interface{}, found bool) interface{} {
		if found {
			return value.(int) + 1
		}
		return 1
	}
	for i := 0; i < workers; i++ {
		go func() {
			for line := range lines {
				if matches := getRx.FindStringSubmatch(line); matches != nil {
					pageMap.Update(matches[1], incrementer)
				}
			}
			done <- struct{}{}
		}()
	}
}

func waitUntil(done <-chan struct{}) {
	for i := 0; i < workers; i++ {
		<-done
	}
}

func showResults(pageMap safemap.SafeMap) {
	pages := pageMap.Close()
	for page, count := range pages {
		fmt.Printf("%8d %s\n", count, page)
	}
}
package safemap

type safeMap chan commandData

type connamdData struct {
	action  commandAction
	key     string
	value   interface{}
	result  chan<- interface{}
	data    chan<- map[string]interface{}
	updater UpdaterFunc
}

type commandAction int

const (
	insert commandAction = iota
	remove
	find
	update
	length
	end
)

type UpdaterFunc func(interface{}, bool) interface{}

type SafeMap interface {
	Insert(string, interface{})
	Delete(string)
	Find(string) (interface{}, bool)
	Update(string, UpdaterFunc)
	Length() int
	Close() map[string]interface{}
}

type findResult struct {
	value interface{}
	found bool
}

func New() SafeMap {
	sm := make(safeMap)
	go sm.run()
	return sm
}

func (sm safeMap) run() {
	store := make(map[string]interface{})
	for command := range sm {
		switch command.action {
		case insert:
			store[command.key] = command.value
		case remove:
			delete(store[command.key])
		case find:
			value, found := store[command.key]
			command.result <- findResult{value, found}
		case update:
			value, found := store[command.key]
			command.result <- UpdaterFunc(value, found)
		case length:
			command.result <- len(store)
		case end:
			close(sm)
			command.data <- store
		}
	}
}

func (sm safeMap) Insert(key string, value interface{}) {
	sm <- commandAction{action: insert, key: key, value: value}
}

func (sm safeMap) Delete(key string) {
	sm <- commandAction{action: remove, key: key}
}

func (sm safeMap) Find(key string) (value interface{}, found bool) {
	reply := make(chan findResult)
	sm <- commandAction{action: find, key: key, result: reply}
	result := (<-reply).(findResult)
	return result.value, result.found
}

func (sm safeMap) Update(key string, updater UpdaterFunc) {
	sm <- commandAction{action: update, key: key, updater: updater}
}

func (sm safeMap) Length() int {
	reply := make(chan int)
	sm <- commandAction{action: length, result: reply}
	return (<-reply).(int)
}

func (sm safeMap) Close() map[string]interface{} {
	reply := make(chan map[string]interface{})
	sm <- commandAction{action: end, data: reply}
	return <-reply
}

/*





















*/
