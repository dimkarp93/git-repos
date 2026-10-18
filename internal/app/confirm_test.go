package app

import (
	"bytes"
	"errors"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const askPrompt = "[yes | yes-to-all | skip | skip-to-all]"

func scripted(input string, assumeYes bool) (*confirmer, *bytes.Buffer) {
	var out bytes.Buffer
	return newScriptedConfirmer(strings.NewReader(input), &out, assumeYes), &out
}

func askAll(t *testing.T, c *confirmer, n int) []answer {
	t.Helper()

	got := make([]answer, 0, n)
	for range n {
		a, err := c.ask("?")
		require.NoError(t, err)
		got = append(got, a)
	}
	return got
}

func TestConfirmerAnswers(t *testing.T) {
	c, _ := scripted("yes\nskip\nskip-to-all\n", false)

	assert.Equal(t, []answer{answerYes, answerSkip, answerQuit}, askAll(t, c, 3))
}

func TestConfirmerYesToAllSticks(t *testing.T) {
	c, out := scripted("yes-to-all\n", false)
	got := askAll(t, c, 3)

	t.Run("answers", func(t *testing.T) {
		assert.Equal(t, []answer{answerYes, answerYes, answerYes}, got)
	})
	t.Run("asked once", func(t *testing.T) {
		assert.Equal(t, 1, strings.Count(out.String(), askPrompt))
	})
}

func TestConfirmerRequiresExplicitAnswer(t *testing.T) {
	c, out := scripted("\n  \nmaybe\nyes\n", false)
	got := askAll(t, c, 1)

	t.Run("answer", func(t *testing.T) {
		assert.Equal(t, []answer{answerYes}, got)
	})
	t.Run("empty hint", func(t *testing.T) {
		assert.Contains(t, out.String(), "pick an option explicitly")
	})
	t.Run("unknown hint", func(t *testing.T) {
		assert.Contains(t, out.String(), "unknown answer")
	})
	t.Run("asked again each time", func(t *testing.T) {
		assert.Equal(t, 4, strings.Count(out.String(), askPrompt))
	})
}

func TestConfirmerAssumeYesSkipsPrompt(t *testing.T) {
	c, out := scripted("", true)
	got := askAll(t, c, 1)

	t.Run("answer", func(t *testing.T) {
		assert.Equal(t, []answer{answerYes}, got)
	})
	t.Run("no prompt", func(t *testing.T) {
		assert.Empty(t, out.String())
	})
}

func TestConfirmerEOFQuits(t *testing.T) {
	c, _ := scripted("", false)

	assert.Equal(t, []answer{answerQuit}, askAll(t, c, 1))
}

func TestConfirmerWithoutTTYFails(t *testing.T) {
	c := &confirmer{out: &bytes.Buffer{}}

	_, err := c.ask("?")

	assert.ErrorIs(t, err, errNotInteractive)
}

type pick struct {
	idx int
	ans answer
}

func chooseAll(t *testing.T, c *confirmer, n int) []pick {
	t.Helper()

	var got []pick
	for range n {
		idx, ans, err := c.choose("?", 3)
		require.NoError(t, err)
		got = append(got, pick{idx: idx, ans: ans})
	}
	return got
}

func TestConfirmerChoose(t *testing.T) {
	c, out := scripted("\n0\n4\n2x\n2\nskip\nq\n", false)
	got := chooseAll(t, c, 3)

	t.Run("answers", func(t *testing.T) {
		assert.Equal(t, []pick{{1, answerYes}, {0, answerSkip}, {0, answerQuit}}, got)
	})
	t.Run("range prompt", func(t *testing.T) {
		assert.Contains(t, out.String(), "[1..3 | skip | skip-to-all]")
	})
}

func TestOptionRange(t *testing.T) {
	assert.Equal(t, []string{"1", "1..3"}, []string{optionRange(1), optionRange(3)})
}

func takenOrigin(name string) error {
	if name == "origin" {
		return errors.New("remote origin already exists")
	}
	return nil
}

func TestConfirmerAskTextDefaultAndValidation(t *testing.T) {
	c, out := scripted("origin\n\n", false)
	got, ans, err := c.askText("Remote name", "github", takenOrigin)

	t.Run("falls back to default", func(t *testing.T) {
		require.NoError(t, err)
		assert.Equal(t, []any{"github", answerYes}, []any{got, ans})
	})
	t.Run("reports invalid name", func(t *testing.T) {
		assert.Contains(t, out.String(), "remote origin already exists")
	})
}

func TestConfirmerAskTextYesToAll(t *testing.T) {
	c, _ := scripted("", true)

	t.Run("valid default", func(t *testing.T) {
		got, ans, err := c.askText("?", "github", takenOrigin)
		require.NoError(t, err)
		assert.Equal(t, []any{"github", answerYes}, []any{got, ans})
	})
	t.Run("invalid default", func(t *testing.T) {
		_, ans, err := c.askText("?", "origin", takenOrigin)
		require.Error(t, err)
		assert.Equal(t, answerSkip, ans)
	})
}

type choice struct {
	got string
	ok  bool
}

func TestConfirmerOption(t *testing.T) {
	c, _ := scripted("\nx\nO\nrename\n", false)
	var got []choice
	for range 3 {
		v, ok, err := c.option("?", "overwrite", "rename")
		require.NoError(t, err)
		got = append(got, choice{got: v, ok: ok})
	}

	assert.Equal(t, []choice{{"overwrite", true}, {"rename", true}, {"", false}}, got)
}

func TestMatchChoice(t *testing.T) {
	cases := map[string]choice{
		"o":         {got: "overwrite", ok: true},
		"rename":    {got: "rename", ok: true},
		"r":         {},
		"overwrite": {got: "overwrite", ok: true},
	}
	for line, want := range cases {
		t.Run(line, func(t *testing.T) {
			got, ok := matchChoice(line, []string{"overwrite", "rename", "remove"})
			assert.Equal(t, want, choice{got: got, ok: ok})
		})
	}
}
