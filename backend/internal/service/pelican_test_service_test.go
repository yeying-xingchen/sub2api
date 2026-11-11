package service

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestParsePelicanSSEOutput(t *testing.T) {
	body := strings.Join([]string{
		`data: {"type":"content","text":"<html><body>"}`,
		`data: {"type":"content","text":"<svg></svg></body></html>"}`,
		`data: {"type":"test_complete","success":true}`,
	}, "\n")
	output, message := parsePelicanSSEOutput(body)
	require.Equal(t, "<html><body><svg></svg></body></html>", output)
	require.Empty(t, message)
}

func TestParsePelicanSSEOutputFailure(t *testing.T) {
	body := `data: {"type":"error","error":"upstream overloaded"}`
	output, message := parsePelicanSSEOutput(body)
	require.Empty(t, output)
	require.Contains(t, message, "upstream overloaded")
}

func TestCleanPelicanOutputStripsCodeFence(t *testing.T) {
	require.Equal(t, "<html><svg></svg></html>", cleanPelicanOutput("```html\n<html><svg></svg></html>\n```"))
	require.Equal(t, "<svg></svg>", cleanPelicanOutput("  <svg></svg>  "))
}

func TestValidatePelicanOutput(t *testing.T) {
	require.Empty(t, validatePelicanOutput("<html><body>ok</body></html>"))
	require.Empty(t, validatePelicanOutput("<svg><circle/></svg>"))
	require.NotEmpty(t, validatePelicanOutput(""))
	require.NotEmpty(t, validatePelicanOutput("here is a pelican"))
}
