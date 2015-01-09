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
