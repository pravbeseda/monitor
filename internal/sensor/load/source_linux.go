package load

import (
	"fmt"
	"os"
	"strconv"
	"strings"
)

const loadavgFile = "/proc/loadavg"

// System returns the load source of the machine the agent runs on.
func System() Source {
	return func() ([3]float64, error) {
		text, err := os.ReadFile(loadavgFile)
		if err != nil {
			return [3]float64{}, err
		}
		return parse(string(text))
	}
}

// parse reads the first three fields of /proc/loadavg: "0.50 1.25 2.12 1/234 5678".
func parse(text string) ([3]float64, error) {
	var averages [3]float64
	fields := strings.Fields(text)
	if len(fields) < len(averages) {
		return averages, fmt.Errorf("%s: want three averages, got %q", loadavgFile, text)
	}
	for i := range averages {
		value, err := strconv.ParseFloat(fields[i], 64)
		if err != nil {
			return averages, fmt.Errorf("%s: %w", loadavgFile, err)
		}
		averages[i] = value
	}
	return averages, nil
}
