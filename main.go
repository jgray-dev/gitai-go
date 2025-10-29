package main

import (
	"bufio"
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

const (
	anthropicAPIURL = "https://api.anthropic.com/v1/messages"
	modelName       = "claude-haiku-4-5-20251001"
	maxCommitLength = 72
	httpTimeout     = 30 * time.Second
	spinnerInterval = 80 * time.Millisecond
)

const (
	colorReset   = "\033[0m"
	colorRed     = "\033[31m"
	colorGreen   = "\033[32m"
	colorYellow  = "\033[33m"
	colorBlue    = "\033[34m"
	colorMagenta = "\033[35m"
	colorCyan    = "\033[36m"
	colorBold    = "\033[1m"
	colorDim     = "\033[2m"
)

var ANTHROPIC_API_KEY = ""

var httpClient = &http.Client{Timeout: httpTimeout}

type AnthropicRequest struct {
	Model     string    `json:"model"`
	MaxTokens int       `json:"max_tokens"`
	Messages  []Message `json:"messages"`
}

type Message struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

type AnthropicResponse struct {
	Content []Content `json:"content"`
	Error   *APIError `json:"error,omitempty"`
}

type Content struct {
	Text string `json:"text"`
}

type APIError struct {
	Type    string `json:"type"`
	Message string `json:"message"`
}

// hello
type FileCommit struct {
	FilePath      string
	Diff          string
	CommitMessage string
	Error         error
}

func main() {
	if ANTHROPIC_API_KEY == "" {
		printError("API key not set")
		fmt.Printf("%sSet ANTHROPIC_API_KEY in main.go:37%s\n", colorDim, colorReset)
		os.Exit(1)
	}

	if err := checkGitRepository(); err != nil {
		printError(err.Error())
		os.Exit(1)
	}

	files, err := getModifiedFiles()
	if err != nil {
		printError(err.Error())
		os.Exit(1)
	}

	if len(files) == 0 {
		printInfo("Nothing to commit")
		return
	}

	printHeader()

	var processing atomic.Int32
	processing.Store(int32(len(files)))
	stopSpinner := make(chan struct{})

	go spinner(&processing, stopSpinner)

	results := processFiles(files, &processing)

	close(stopSpinner)
	clearLine()

	displayAndCommit(results)
}

func checkGitRepository() error {
	if err := exec.Command("git", "rev-parse", "--git-dir").Run(); err != nil {
		return fmt.Errorf("not a git repository")
	}
	return nil
}

func getModifiedFiles() ([]string, error) {
	hasCommits := exec.Command("git", "rev-parse", "HEAD").Run() == nil

	var cmd *exec.Cmd
	if hasCommits {
		cmd = exec.Command("git", "diff", "--name-only", "HEAD")
	} else {
		cmd = exec.Command("git", "ls-files", "--others", "--exclude-standard")
	}

	output, err := cmd.Output()
	if err != nil {
		if exitErr, ok := err.(*exec.ExitError); ok {
			return nil, fmt.Errorf("git error: %s", strings.TrimSpace(string(exitErr.Stderr)))
		}
		return nil, err
	}

	var files []string
	scanner := bufio.NewScanner(bytes.NewReader(output))
	for scanner.Scan() {
		if file := strings.TrimSpace(scanner.Text()); file != "" {
			files = append(files, file)
		}
	}

	return files, nil
}

func processFiles(files []string, processing *atomic.Int32) []FileCommit {
	var wg sync.WaitGroup
	results := make([]FileCommit, len(files))

	for i, file := range files {
		wg.Add(1)
		go func(idx int, filePath string) {
			defer wg.Done()
			results[idx] = processFile(filePath)
			processing.Add(-1)
		}(i, file)
	}

	wg.Wait()
	return results
}

func processFile(filePath string) FileCommit {
	diff, err := getFileDiff(filePath)
	if err != nil {
		return FileCommit{FilePath: filePath, Error: err}
	}

	commitMsg, err := generateCommitMessage(diff, filePath)
	if err != nil {
		return FileCommit{FilePath: filePath, Diff: diff, Error: err}
	}

	return FileCommit{
		FilePath:      filePath,
		Diff:          diff,
		CommitMessage: commitMsg,
	}
}

func getFileDiff(filePath string) (string, error) {
	hasCommits := exec.Command("git", "rev-parse", "HEAD").Run() == nil

	var cmd *exec.Cmd
	if hasCommits {
		cmd = exec.Command("git", "diff", "HEAD", "--", filePath)
	} else {
		exec.Command("git", "add", "--intent-to-add", filePath).Run()
		cmd = exec.Command("git", "diff", "--", filePath)
	}

	output, err := cmd.Output()
	if err != nil {
		return "", fmt.Errorf("diff failed: %w", err)
	}

	if len(output) == 0 {
		content, err := os.ReadFile(filePath)
		if err != nil {
			return "", fmt.Errorf("read failed: %w", err)
		}
		return string(content), nil
	}

	return string(output), nil
}

func generateCommitMessage(diff, filePath string) (string, error) {
	prompt := fmt.Sprintf(`Generate a concise git commit message for this diff.

RULES:
- Max %d characters total
- Imperative mood ("Add" not "Added")
- Specific and clear
- Focus on WHAT and WHY
- No formatting, just the message

File: %s
Diff: %s

Message:`, maxCommitLength, filePath, diff)

	reqBody := AnthropicRequest{
		Model:     modelName,
		MaxTokens: 100,
		Messages:  []Message{{Role: "user", Content: prompt}},
	}

	jsonData, err := json.Marshal(reqBody)
	if err != nil {
		return "", err
	}

	req, err := http.NewRequest("POST", anthropicAPIURL, bytes.NewBuffer(jsonData))
	if err != nil {
		return "", err
	}

	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("x-api-key", ANTHROPIC_API_KEY)
	req.Header.Set("anthropic-version", "2023-06-01")

	resp, err := httpClient.Do(req)
	if err != nil {
		return "", fmt.Errorf("API call failed: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", err
	}

	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("API error %d: %s", resp.StatusCode, body)
	}

	var apiResp AnthropicResponse
	if err := json.Unmarshal(body, &apiResp); err != nil {
		return "", err
	}

	if apiResp.Error != nil {
		return "", fmt.Errorf("%s: %s", apiResp.Error.Type, apiResp.Error.Message)
	}

	if len(apiResp.Content) == 0 {
		return "", fmt.Errorf("empty response")
	}

	msg := strings.TrimSpace(apiResp.Content[0].Text)
	if len(msg) > maxCommitLength {
		msg = msg[:maxCommitLength-3] + "..."
	}

	return msg, nil
}

func stageAndCommitFile(filePath, message string) error {
	if err := exec.Command("git", "add", filePath).Run(); err != nil {
		return err
	}
	if err := exec.Command("git", "commit", "-m", message).Run(); err != nil {
		return err
	}
	return nil
}

func displayAndCommit(results []FileCommit) {
	var successCount, failCount int

	fmt.Println()

	for _, commit := range results {
		if commit.Error != nil {
			printFile(commit.FilePath)
			fmt.Printf("%s  │ %s%s\n\n", colorDim, colorRed, commit.Error, colorReset)
			failCount++
			continue
		}

		printFile(commit.FilePath)
		printCommitMsg(commit.CommitMessage)

		if err := stageAndCommitFile(commit.FilePath, commit.CommitMessage); err != nil {
			fmt.Printf("%s  │ %sfailed: %v%s\n\n", colorDim, colorRed, err, colorReset)
			failCount++
		} else {
			successCount++
		}
		fmt.Println()
	}

	printSummary(successCount, failCount)
}

func spinner(processing *atomic.Int32, stop chan struct{}) {
	frames := []string{"⠋", "⠙", "⠹", "⠸", "⠼", "⠴", "⠦", "⠧", "⠇", "⠏"}
	messages := []string{
		"Analyzing diffs",
		"Consulting Claude",
		"Crafting messages",
		"Processing changes",
	}
	i, msgIndex := 0, 0
	ticker := time.NewTicker(spinnerInterval)
	msgTicker := time.NewTicker(2 * time.Second)
	defer ticker.Stop()
	defer msgTicker.Stop()

	for {
		select {
		case <-stop:
			return
		case <-msgTicker.C:
			msgIndex = (msgIndex + 1) % len(messages)
		case <-ticker.C:
			remaining := processing.Load()
			fmt.Printf("\r%s%s%s %s%s...%s [%d remaining]",
				colorCyan, frames[i], colorReset,
				colorBold, messages[msgIndex], colorDim, remaining)
			i = (i + 1) % len(frames)
		}
	}
}

func clearLine() {
	fmt.Print("\r\033[K")
}

func printHeader() {
	fmt.Printf("\n%s%sgitai%s %sAI-powered commits%s\n\n", colorBold, colorMagenta, colorReset, colorDim, colorReset)
}

func printSectionHeader(title string) {
	fmt.Printf("%s%s▸ %s%s\n", colorBold, colorCyan, title, colorReset)
}

func printSuccess(msg string) {
	fmt.Printf("%s%s%s\n", colorGreen, msg, colorReset)
}

func printError(msg string) {
	fmt.Printf("%s%s%s\n", colorRed, msg, colorReset)
}

func printInfo(msg string) {
	fmt.Printf("%s%s%s\n", colorCyan, msg, colorReset)
}

func printFile(filename string) {
	fmt.Printf("%s▸ %s%s\n", colorBlue, filename, colorReset)
}

func printCommitMsg(msg string) {
	fmt.Printf("%s  │ %s%s\n", colorDim, msg, colorReset)
}

func printSummary(success, failed int) {
	total := success + failed
	if failed == 0 {
		fmt.Printf("%s%d/%d committed%s\n", colorGreen, success, total, colorReset)
	} else {
		fmt.Printf("%s%d succeeded%s %s│%s %s%d failed%s\n",
			colorGreen, success, colorReset,
			colorDim, colorReset,
			colorRed, failed, colorReset)
	}
	fmt.Println()
}
