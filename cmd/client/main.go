package main

import (
	"bufio"
	"context"
	"errors"
	"flag"
	"fmt"
	"net"
	"os"
	"powTcp/internal/pow"
	"runtime"
	"strconv"
	"strings"
	"time"
)

func parseChallenge(line string) (token string, difficulty int, err error) {
	fields := strings.Fields(line)
	if len(fields) != 2 || fields[0] != "CHALLENGE" {
		return "", 0, errors.New("expected: CHALLENGE <token>")
	}
	token = fields[1]

	parts := strings.Split(token, ".") // v1.<difficulty>.<expires>.<random_hex>.<mac_hex>
	if len(parts) != 5 || parts[0] != "v1" {
		return "", 0, errors.New("malformed token")
	}

	difficulty, err = strconv.Atoi(parts[1])
	if err != nil {
		return "", 0, fmt.Errorf("bad difficulty: %w", err)
	}
	return token, difficulty, nil
}

func run(addr string, solveTimeout time.Duration) error {
	conn, err := net.DialTimeout("tcp", addr, time.Second*5)
	if err != nil {
		return err
	}
	defer conn.Close()

	reader := bufio.NewReader(conn)
	_ = conn.SetReadDeadline(time.Now().Add(time.Second * 30))
	line, err := reader.ReadString('\n')
	if err != nil {
		return fmt.Errorf("read challenge: %w", err)
	}

	token, difficulty, err := parseChallenge(line)
	if err != nil {
		return err
	}

	ctx, cancel := context.WithTimeout(context.Background(), solveTimeout)
	defer cancel()

	counter, err := pow.SolveParallel(ctx, token, difficulty, runtime.NumCPU())
	if errors.Is(err, context.DeadlineExceeded) {
		return errors.New("couldn't solve in 5 seconds")
	} else if err != nil {
		return err
	}

	_ = conn.SetWriteDeadline(time.Now().Add(time.Second * 30))
	if _, err = fmt.Fprintf(conn, "SOLUTION %s %d\n", token, counter); err != nil {
		return fmt.Errorf("send solution: %w", err)
	}
	resp, err := reader.ReadString('\n')
	if err != nil {
		return err
	}

	resp = strings.TrimSpace(resp)
	if quote, ok := strings.CutPrefix(resp, "QUOTE "); ok {
		fmt.Println(quote)
		return nil
	}

	return fmt.Errorf("server error: %s", resp)
}

func main() {
	// CHALLENGE v1.<difficulty>.<expires>.<random_hex>.<mac_hex>
	// CHALLENGE v1.18.1792262311.dd18fcec00028a9b62400426cc849d20.9383d22ab1005452cb4d102ece9c8712cbcd67cbed7898786561c9d0f7c0ac04

	addr := flag.String("addr", "127.0.0.1:8090", "server address")
	solveTimeout := flag.Duration("timeout", time.Second*5, "max time to solve the challenge")
	flag.Parse()

	if err := run(*addr, *solveTimeout); err != nil {
		_, _ = fmt.Fprintln(os.Stderr, "error:", err)
		os.Exit(1)
	}
}
