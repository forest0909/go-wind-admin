package captcha

import (
	"fmt"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestRandomMathQuestion(t *testing.T) {
	seen := map[string]bool{}
	for i := 0; i < 2000; i++ {
		q, ans := randomMathQuestion()
		require.True(t, strings.HasSuffix(q, " = ?"), q)

		var a, b int
		var op string
		_, err := fmt.Sscanf(strings.TrimSuffix(q, " = ?"), "%d %s %d", &a, &op, &b)
		require.NoError(t, err, q)
		seen[op] = true

		switch op {
		case "+":
			assert.Equal(t, a+b, ans, q)
		case "-":
			assert.Equal(t, a-b, ans, q)
		case "×":
			assert.Equal(t, a*b, ans, q)
		case "÷":
			require.NotZero(t, b, q)
			assert.Zero(t, a%b, q)
			assert.Equal(t, a/b, ans, q)
		default:
			t.Fatalf("unexpected operator in %q", q)
		}
		assert.GreaterOrEqual(t, ans, 0, q)
	}
	assert.Len(t, seen, 4)
}

func TestDrawMathImage(t *testing.T) {
	b64s, err := drawMathImage("18 ÷ 3 = ?", 240, 80)
	require.NoError(t, err)
	assert.True(t, strings.HasPrefix(b64s, "data:image/png;base64,"))
}
