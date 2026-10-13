package main

import (
	"encoding/csv"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"sort"
	"strconv"
	"strings"
	"time"
)

type row struct {
	ts      string
	status  int
	version string
	pod     string
	latency float64
	err     string
}

func main() {
	var (
		urlStr   = flag.String("url", "http://localhost:30080/", "service URL")
		rps      = flag.Float64("rps", 10.0, "requests per second")
		duration = flag.Duration("duration", 0, "total load duration (e.g. 60s)")
		timeout  = flag.Duration("timeout", 2*time.Second, "per-request timeout")
		out      = flag.String("out", "", "output CSV path (required)")
	)
	flag.Parse()
	if *out == "" || *duration <= 0 {
		fmt.Fprintln(os.Stderr, "both --duration and --out are required")
		flag.Usage()
		os.Exit(2)
	}

	client := &http.Client{
		Timeout: *timeout,
		Transport: &http.Transport{
			DisableKeepAlives: true,
		},
	}
	interval := time.Duration(float64(time.Second) / *rps)
	deadline := time.Now().Add(*duration)
	var rows []row

	nextSlot := time.Now()
	for time.Now().Before(deadline) {
		start := time.Now()
		r := doRequest(client, *urlStr)
		r.latency = float64(time.Since(start).Microseconds()) / 1000.0
		rows = append(rows, r)

		nextSlot = nextSlot.Add(interval)
		if wait := time.Until(nextSlot); wait > 0 {
			time.Sleep(wait)
		}
	}

	if err := writeCSV(*out, rows); err != nil {
		fmt.Fprintln(os.Stderr, "write csv:", err)
		os.Exit(1)
	}
	summary := summarize(rows)
	fmt.Println(summary)
	if err := os.WriteFile(*out+".summary", []byte(summary+"\n"), 0o644); err != nil {
		fmt.Fprintln(os.Stderr, "write summary:", err)
		os.Exit(1)
	}
}

func doRequest(client *http.Client, urlStr string) row {
	r := row{ts: time.Now().Format("15:04:05")}
	resp, err := client.Get(urlStr)
	if err != nil {
		r.err = classify(err)
		if resp != nil {
			r.status = resp.StatusCode
		}
		return r
	}
	defer resp.Body.Close()
	r.status = resp.StatusCode
	body, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	var data struct {
		Version string `json:"version"`
		Pod     string `json:"pod"`
	}
	if json.Unmarshal(body, &data) == nil {
		r.version = data.Version
		r.pod = data.Pod
	}
	return r
}

func classify(err error) string {
	if uerr, ok := errors.AsType[*url.Error](err); ok {
		err = uerr.Err
	}
	var nerr net.Error
	if errors.As(err, &nerr) && nerr.Timeout() {
		return "timeout"
	}
	var opErr *net.OpError
	if errors.As(err, &opErr) {
		if errors.Is(opErr.Err, net.ErrClosed) {
			return "connection_refused"
		}
		var sysErr *os.SyscallError
		if errors.As(opErr.Err, &sysErr) {
			return "connection_refused"
		}
	}
	return "other"
}

func writeCSV(path string, rows []row) error {
	f, err := os.Create(path)
	if err != nil {
		return err
	}
	defer f.Close()
	w := csv.NewWriter(f)
	defer w.Flush()
	if err := w.Write([]string{"time", "status", "version", "pod", "latency_ms", "error"}); err != nil {
		return err
	}
	for _, r := range rows {
		if err := w.Write([]string{
			r.ts,
			strconv.Itoa(r.status),
			r.version,
			r.pod,
			strconv.FormatFloat(r.latency, 'f', 1, 64),
			r.err,
		}); err != nil {
			return err
		}
	}
	return w.Error()
}

func summarize(rows []row) string {
	total := len(rows)
	ok := 0
	byErr := map[string]int{}
	byVersion := map[string]int{}
	for _, r := range rows {
		if r.err == "" && r.status == http.StatusOK {
			ok++
		}
		if r.err != "" {
			byErr[r.err]++
		}
		if r.version != "" {
			byVersion[r.version]++
		}
	}
	errKeys := sortedKeys(byErr)
	verKeys := sortedKeys(byVersion)
	errParts := make([]string, 0, len(errKeys))
	for _, k := range errKeys {
		errParts = append(errParts, fmt.Sprintf("%q: %d", k, byErr[k]))
	}
	verParts := make([]string, 0, len(verKeys))
	for _, k := range verKeys {
		verParts = append(verParts, fmt.Sprintf("%q: %d", k, byVersion[k]))
	}
	return fmt.Sprintf(
		"total=%d ok=%d errors=%d\nerror_types={%s}\nversions={%s}",
		total, ok, total-ok,
		strings.Join(errParts, ", "),
		strings.Join(verParts, ", "),
	)
}

func sortedKeys(m map[string]int) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}
