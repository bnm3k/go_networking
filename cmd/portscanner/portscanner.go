package main

import (
	"errors"
	"flag"
	"fmt"
	"net"
	"os"
	"os/signal"
	"strconv"
	"sync"
	"syscall"
	"time"

	"github.com/cheggaaa/pb/v3"
)

type scanResult struct {
	protocol string
	port     int
	isOpen   bool
}

func scanPort(protocol, hostname, port string) bool {
	address := hostname + ":" + port
	conn, err := net.DialTimeout(protocol, address, 60*time.Second)
	if err != nil {
		return false
	}
	defer conn.Close()
	return true
}

func concurrentScan(protocol, hostname string, minPort, maxPort int, concurrencyLimiterCh chan struct{}, resultsCh chan<- *scanResult) {
	var wg sync.WaitGroup
	totalPortsNum := maxPort - minPort + 1
	wg.Add(totalPortsNum)
	for port := minPort; port <= maxPort; port++ {
		<-concurrencyLimiterCh
		go func(port int) {
			portStr := strconv.Itoa(port)
			isOpen := scanPort(protocol, hostname, portStr)
			concurrencyLimiterCh <- struct{}{}
			resultsCh <- &scanResult{protocol, port, isOpen}
			wg.Done()
		}(port)
	}
	wg.Wait()
	close(resultsCh)
}

func getConcurrencyLimiter(max int) chan struct{} {
	concurrencyLimitCh := make(chan struct{}, max)
	for i := 0; i < max; i++ {
		concurrencyLimitCh <- struct{}{}
	}
	return concurrencyLimitCh
}

type config struct {
	minPort       int
	maxPort       int
	maxConcurrent int
	hostname      string
	protocol      string
}

func getConfig() *config {
	cfg := new(config)
	flag.IntVar(&cfg.minPort, "min", 0, "min port to start scanning from")

	flag.IntVar(&cfg.maxPort, "max", 65535, "max port to end scanning from")

	flag.IntVar(&cfg.maxConcurrent, "concurrency", 50, "max concurrent ports to be scanning at a go")

	flag.StringVar(&cfg.hostname, "h", "", "hostname")

	flag.StringVar(&cfg.protocol, "proto", "tcp", "[tcp/udp]")

	return cfg
}

func validateConfig(cfg *config) error {
	if cfg.minPort < 0 {
		return errors.New("Invalid min port, should be >= 0")
	}
	if cfg.maxPort > 65535 {
		return errors.New("Invalid max port, should be <= 65535")
	}
	if cfg.maxPort < cfg.minPort {
		return fmt.Errorf("Invalid min(%d) and max port(%d)", cfg.minPort, cfg.maxPort)
	}
	if cfg.maxConcurrent < 0 {
		return errors.New("Invalid max concurrent")
	}
	if cfg.hostname == "" {
		return errors.New("Provide hostname")
	}
	if cfg.protocol != "tcp" && cfg.protocol != "udp" {
		return fmt.Errorf("Invalid protocol: %s", cfg.protocol)
	}
	return nil
}

func main() {
	// config
	cfg := getConfig()
	flag.Parse()
	err := validateConfig(cfg)
	if err != nil {
		fmt.Println(err)
		os.Exit(1)
	}

	// listen for ctrl-c
	c := make(chan os.Signal, 1)
	signal.Notify(c, os.Interrupt, syscall.SIGTERM)
	go func() {
		<-c
		fmt.Println("\nctrl-c?")
		os.Exit(1)
	}()

	fmt.Println("Scanning ports...")

	resultsCh := make(chan *scanResult, 1)
	concurrencyLimitCh := getConcurrencyLimiter(cfg.maxConcurrent)
	defer close(concurrencyLimitCh)

	go concurrentScan(cfg.protocol, cfg.hostname, cfg.minPort, cfg.maxPort, concurrencyLimitCh, resultsCh)

	count := cfg.maxPort - cfg.minPort + 1
	bar := pb.StartNew(count)
	bar.SetMaxWidth(100)

	var results []string
	for res := range resultsCh {
		bar.Add(1)
		if res.isOpen {
			resStr := fmt.Sprintf("%s %5d = open", res.protocol, res.port)
			results = append(results, resStr)
		}
	}
	bar.Finish()
	fmt.Printf("%d/%d %s ports open. Host: %s. Range %d-%d\n", len(results), count, cfg.protocol, cfg.hostname, cfg.minPort, cfg.maxPort)
	for _, resStr := range results {
		fmt.Println(resStr)
	}
}
