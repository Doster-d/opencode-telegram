package main

import (
	"bufio"
	"flag"
	"fmt"
	"os"
	"strconv"
	"strings"
)

func main() {
	filePath := flag.String("file", "coverage.out", "path to go coverage profile")
	min := flag.Float64("min", 90.0, "minimum required total coverage percent")
	flag.Parse()

	total, covered, err := readCoverage(*filePath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "coveragecheck: %v\n", err)
		os.Exit(1)
	}
	if total == 0 {
		fmt.Fprintln(os.Stderr, "coveragecheck: no statements found in coverage profile")
		os.Exit(1)
	}

	pct := (covered / total) * 100
	fmt.Printf("total coverage: %.1f%% (min %.1f%%)\n", pct, *min)
	if pct < *min {
		fmt.Fprintln(os.Stderr, "coveragecheck: threshold not met")
		os.Exit(1)
	}
}

func readCoverage(path string) (float64, float64, error) {
	f, err := os.Open(path)
	if err != nil {
		return 0, 0, err
	}
	defer f.Close()

	var total float64
	var covered float64

	scanner := bufio.NewScanner(f)
	lineNo := 0
	for scanner.Scan() {
		lineNo++
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}
		if lineNo == 1 {
			if !strings.HasPrefix(line, "mode:") {
				return 0, 0, fmt.Errorf("invalid coverage profile header: %q", line)
			}
			continue
		}

		fields := strings.Fields(line)
		if len(fields) < 3 {
			return 0, 0, fmt.Errorf("invalid coverage line %d: %q", lineNo, line)
		}

		numStmts, err := strconv.ParseFloat(fields[len(fields)-2], 64)
		if err != nil {
			return 0, 0, fmt.Errorf("invalid statement count at line %d: %w", lineNo, err)
		}
		execCount, err := strconv.ParseFloat(fields[len(fields)-1], 64)
		if err != nil {
			return 0, 0, fmt.Errorf("invalid execution count at line %d: %w", lineNo, err)
		}

		total += numStmts
		if execCount > 0 {
			covered += numStmts
		}
	}

	if err := scanner.Err(); err != nil {
		return 0, 0, err
	}

	return total, covered, nil
}
