package pow

import (
	"context"
	"crypto/sha256"
	"fmt"
	"math/bits"
	"runtime"
	"strconv"
	"sync"
)

const MaxDifficulty = 32

func leadingZeroBits(sum []byte) int {
	var totalZeros int

	for _, b := range sum {
		zeros := bits.LeadingZeros8(b)
		totalZeros += zeros
		if zeros < 8 {
			return totalZeros
		}
	}

	return totalZeros
}

func Solve(ctx context.Context, token string, difficulty int) (uint64, error) {
	if difficulty < 0 || difficulty > MaxDifficulty {
		return 0, fmt.Errorf("unsupported difficulty: %d", difficulty)
	}

	counter, ok := search(ctx, token, difficulty, 0, 1)
	if !ok {
		return 0, ctx.Err()
	}
	return counter, nil
}

func SolveParallel(ctx context.Context, token string, difficulty, workers int) (uint64, error) {
	if difficulty < 0 || difficulty > MaxDifficulty {
		return 0, fmt.Errorf("unsupported difficulty: %d", difficulty)
	}
	if workers < 1 {
		workers = runtime.NumCPU()
	}

	ctx, cancel := context.WithCancel(ctx)
	defer cancel()

	result := make(chan uint64, 1)
	var wg sync.WaitGroup

	for i := range workers {
		wg.Go(func() {
			if counter, ok := search(ctx, token, difficulty, uint64(i), uint64(workers)); ok {
				select {
				case result <- counter:
				default:
				}
				cancel()
			}
		})
	}

	wg.Wait()

	select {
	case counter := <-result:
		return counter, nil
	default:
		return 0, ctx.Err()
	}
}

func search(ctx context.Context, token string, difficulty int, start, stride uint64) (uint64, bool) {
	buffer := make([]byte, 0, len(token)+1+20)
	buffer = append(buffer, token...)
	buffer = append(buffer, ':')
	prefixLength := len(buffer)

	counter := start
	for n := 0; ; n++ {
		if n%1024 == 0 && ctx.Err() != nil {
			return 0, false
		}

		buffer = strconv.AppendUint(buffer[:prefixLength], counter, 10)
		sum := sha256.Sum256(buffer)
		if leadingZeroBits(sum[:]) >= difficulty {
			return counter, true
		}
		counter += stride
	}
}

func Check(token string, counter uint64, difficulty int) bool {
	sum := sha256.Sum256([]byte(token + ":" + strconv.FormatUint(counter, 10)))
	return leadingZeroBits(sum[:]) >= difficulty
}
