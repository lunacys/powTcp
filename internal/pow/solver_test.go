package pow

import (
	"context"
	"crypto/sha256"
	"fmt"
	"strconv"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"go.uber.org/goleak"
)

func BenchmarkSolve(b *testing.B) {
	token := "v1.12.1792000000.9f3a1c7e5b2d4a60.080edd534819db9b"
	for b.Loop() {
		_, _ = Solve(context.Background(), token, 12)
	}
}

func TestSolveParallel(t *testing.T) {
	token := "v1.12.1792000000.9f3a1c7e5b2d4a60.080edd534819db9b"

	counter, err := SolveParallel(context.Background(), token, 12, 4)
	require.NoError(t, err)

	sum := sha256.Sum256([]byte(token + ":" + strconv.FormatUint(counter, 10)))
	require.GreaterOrEqual(t, leadingZeroBits(sum[:]), 12)
}

func TestSolveParallelCancelled(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	_, err := SolveParallel(ctx, "token", 32, 4)
	require.ErrorIs(t, err, context.Canceled)
}

func BenchmarkSolveParallel(b *testing.B) {
	token := "v1.12.1792000000.9f3a1c7e5b2d4a60.080edd534819db9b"

	for _, workers := range []int{1, 2, 4, 8, 16} {
		b.Run(fmt.Sprintf("workers=%d", workers), func(b *testing.B) {
			for b.Loop() {
				if _, err := SolveParallel(context.Background(), token, 22, workers); err != nil {
					b.Fatal(err)
				}
			}
		})
	}
}

func TestSolveParallelTimeout(t *testing.T) {
	defer goleak.VerifyNone(t) // проверка, что горутины не утекли

	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()

	_, err := SolveParallel(ctx, "token", 32, 4)

	require.ErrorIs(t, err, context.DeadlineExceeded)
}
