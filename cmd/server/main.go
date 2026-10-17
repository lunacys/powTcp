package main

import (
	"bufio"
	"context"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"os"
	"os/signal"
	"powTcp/internal/pow"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"
)

const maxConnections = 16

var (
	errMalformed = errors.New("malformed token")
	errBadMAC    = errors.New("bad mac")
	errExpired   = errors.New("expired")
)

type challenge struct {
	Difficulty int
	Expires    time.Time
	ID         string
}

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	var wg sync.WaitGroup
	cache := pow.NewReplayCache(time.Now)
	sem := make(chan struct{}, maxConnections)

	secret, err := generateTestSecret()
	if err != nil {
		log.Fatal(err)
	}

	wg.Go(func() {
		cache.Run(ctx, time.Second*10)
	})

	listener, err := net.Listen("tcp", ":8090")
	if err != nil {
		log.Fatal("Error listening on port 8090:", err)
	}

	go func() {
		<-ctx.Done()
		log.Println("Closing listener")
		listener.Close()
	}()

	log.Printf("Listening on port 8090")
	for {
		conn, err := listener.Accept()
		if err != nil {
			if ctx.Err() != nil || errors.Is(err, net.ErrClosed) {
				break
			}
			log.Println("Error accepting connection:", err)
			continue
		}

		select {
		case sem <- struct{}{}:
		default:
			conn.Close()
			continue
		}
		wg.Go(func() {
			log.Printf("Accepted new connection from %s", conn.RemoteAddr())
			defer func() {
				<-sem
			}()
			handleConnection(conn, ctx, secret, cache)
		})
	}

	wg.Wait()
}

func handleConnection(conn net.Conn, ctx context.Context, secret []byte, cache *pow.ReplayCache) {
	defer conn.Close()
	defer func() {
		log.Printf("Closing connection from %s", conn.RemoteAddr())
		if r := recover(); r != nil {
			log.Printf("panic is handled: %v", r)
		}
	}()

	stop := context.AfterFunc(ctx, func() { conn.Close() })
	defer stop()

	reader := bufio.NewReaderSize(conn, 1024)

	clientIP, _, err := net.SplitHostPort(conn.RemoteAddr().String())
	if err != nil {
		return
	}

	token, err := generateToken(18, clientIP, time.Second*30, time.Now, secret)
	if err != nil {
		return
	}

	reply(conn, "CHALLENGE "+token)

	_ = conn.SetReadDeadline(time.Now().Add(30 * time.Second))
	line, err := reader.ReadSlice('\n')
	if err != nil {
		switch {
		case errors.Is(err, bufio.ErrBufferFull):
			_, _ = fmt.Fprint(conn, "ERR line_too_long\n")
		case errors.Is(err, os.ErrDeadlineExceeded):
			log.Printf("Read timeout")
		case errors.Is(err, io.EOF), errors.Is(err, net.ErrClosed):
		default:
			log.Println("Error reading from connection:", err)
		}
		return
	}

	fields := strings.Fields(string(line))
	if len(fields) != 3 || fields[0] != "SOLUTION" {
		reply(conn, "ERR bad_request")
		return
	}

	counter, err := strconv.ParseUint(fields[2], 10, 64)
	if err != nil {
		reply(conn, "ERR bad_request")
		return
	}

	chal, err := verifyToken(fields[1], clientIP, secret, time.Now)
	switch {
	case errors.Is(err, errExpired):
		reply(conn, "ERR expired")
		return
	case err != nil:
		reply(conn, "ERR bad_request")
		return
	}

	if !pow.Check(fields[1], counter, chal.Difficulty) {
		reply(conn, "ERR invalid_pow")
		return
	}

	if !cache.TryAdd(chal.ID, chal.Expires) {
		reply(conn, "ERR replay")
		return
	}

	quote, err := getRandomQuote(ctx)
	if err != nil {
		log.Printf("quote fetch failed: %v", err)
		reply(conn, "ERR unavailable")
		return
	}

	reply(conn, "QUOTE "+quote)
}

type quoteResponse struct {
	Quote  string `json:"q"`
	Author string `json:"a"`
}

const quoteURL = "https://zenquotes.io/api/random"

var quoteClient = &http.Client{Timeout: time.Second * 3}

func getRandomQuote(ctx context.Context) (string, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, quoteURL, nil)
	if err != nil {
		return "", err
	}

	resp, err := quoteClient.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("zenquotes: unexpected status code: %d", resp.StatusCode)
	}

	var quotes []quoteResponse
	if err := json.NewDecoder(io.LimitReader(resp.Body, 64<<10)).Decode(&quotes); err != nil {
		return "", err
	}

	if len(quotes) == 0 || strings.TrimSpace(quotes[0].Quote) == "" {
		return "", errors.New("zenquotes: empty response")
	}

	return strings.Join(strings.Fields(quotes[0].Quote), " "), nil
}

func reply(conn net.Conn, msg string) {
	_ = conn.SetWriteDeadline(time.Now().Add(5 * time.Second))
	if _, err := fmt.Fprintf(conn, "%s\n", msg); err != nil {
		return
	}
}

func generateToken(difficulty int, clientIP string, ttl time.Duration, now func() time.Time, secret []byte) (string, error) {
	expires := now().Add(ttl).Unix()
	buf := make([]byte, 16)
	if _, err := rand.Read(buf); err != nil {
		return "", err
	}

	body := fmt.Sprintf("v1.%d.%d.%x", difficulty, expires, buf)
	mac := sign(secret, body, clientIP)

	// v1.<difficulty>.<expires_unix>.<random_hex>.<mac_hex>
	return body + "." + mac, nil
}

func sign(secret []byte, body, clientIP string) string {
	// mac = HMAC-SHA256(secret, version.difficulty.expires.random.clientIP)
	h := hmac.New(sha256.New, secret)
	h.Write([]byte(body + "." + clientIP))
	return hex.EncodeToString(h.Sum(nil))
}

func generateTestSecret() ([]byte, error) {
	buf := make([]byte, 16)
	if _, err := rand.Read(buf); err != nil {
		return buf, err
	}
	return buf, nil
}

func verifyToken(token, clientIP string, secret []byte, now func() time.Time) (challenge, error) {
	idx := strings.LastIndexByte(token, '.')
	if idx < 0 {
		return challenge{}, errMalformed
	}
	body, macHex := token[:idx], token[idx+1:]

	got, err := hex.DecodeString(macHex)
	if err != nil {
		return challenge{}, errMalformed
	}

	h := hmac.New(sha256.New, secret)
	h.Write([]byte(body + "." + clientIP))
	if !hmac.Equal(got, h.Sum(nil)) {
		return challenge{}, errBadMAC
	}

	parts := strings.Split(body, ".") // v1.difficulty.expires.random
	if len(parts) != 4 || parts[0] != "v1" {
		return challenge{}, errMalformed
	}

	difficulty, err := strconv.Atoi(parts[1])
	if err != nil {
		return challenge{}, errMalformed
	}

	expires, err := strconv.ParseInt(parts[2], 10, 64)
	if err != nil {
		return challenge{}, errMalformed
	}

	if now().Unix() > expires {
		return challenge{}, errExpired
	}

	return challenge{
		Difficulty: difficulty,
		Expires:    time.Unix(expires, 0),
		ID:         parts[3],
	}, nil
}
