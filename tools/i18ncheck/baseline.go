package main

import (
	"errors"
	"fmt"
	"os"
	"slices"
	"strconv"
	"strings"
)

const baselineFields = 2

var (
	errMalformedLine = errors.New("malformed baseline line")
	errNotPositive   = errors.New("count must be a positive number")
	errDuplicateLine = errors.New("package listed twice")
)

type baseline map[string]int

func readBaseline(path string) (baseline, error) {
	content, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("cannot read baseline %s: %w", path, err)
	}
	allowed := make(baseline)
	for number, line := range strings.Split(string(content), "\n") {
		if strings.TrimSpace(line) == "" {
			continue
		}
		key, count, err := parseBaselineLine(line)
		if err != nil {
			return nil, fmt.Errorf("baseline %s line %d: %w", path, number+1, err)
		}
		if _, listed := allowed[key]; listed {
			return nil, fmt.Errorf("baseline %s line %d: %s: %w", path, number+1, key, errDuplicateLine)
		}
		allowed[key] = count
	}
	return allowed, nil
}

func parseBaselineLine(line string) (string, int, error) {
	fields := strings.Fields(line)
	if len(fields) != baselineFields {
		return "", 0, fmt.Errorf("%w: want \"<package> <count>\", got %q", errMalformedLine, line)
	}
	count, err := strconv.Atoi(fields[1])
	if err != nil || count <= 0 {
		return "", 0, fmt.Errorf("%s: %w; delete the line of a package at zero", fields[0], errNotPositive)
	}
	return fields[0], count, nil
}

func packagesOf(found map[string][]finding, allowed baseline) []string {
	packages := make([]string, 0, len(found)+len(allowed))
	for key := range found {
		packages = append(packages, key)
	}
	for key := range allowed {
		if _, seen := found[key]; !seen {
			packages = append(packages, key)
		}
	}
	slices.Sort(packages)
	return packages
}
