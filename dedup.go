package main

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"syscall"
	"time"
)

const inflightDirName = "plannotator-review-gate.inflight"

type sharedVerdict struct {
	Decision *Decision `json:"decision"`
}

type coordinatedReview struct {
	Decision *Decision
	Entry    string
	Primary  bool
}

func coordinateReview(ctx context.Context, ev *Event, review func() *Decision) coordinatedReview {
	root := filepath.Join(claudeDir(), inflightDirName)
	if err := os.MkdirAll(root, 0o700); err != nil {
		return primaryCoordination("", gateBroken(
			fmt.Sprintf("could not create the review coordination directory (%v)", err),
		))
	}
	pruneInflight(root)

	entry := reviewEntry(root, ev)
	err := os.Mkdir(entry, 0o700)
	switch {
	case err == nil:
		return leadReview(entry, review)
	case os.IsExist(err):
		return awaitReview(ctx, entry)
	default:
		return primaryCoordination(entry, gateBroken(
			fmt.Sprintf("could not coordinate duplicate review hooks (%v)", err),
		))
	}
}

func primaryCoordination(entry string, decision *Decision) coordinatedReview {
	return coordinatedReview{Decision: decision, Entry: entry, Primary: true}
}

func reviewEntry(root string, ev *Event) string {
	sum := sha256.Sum256([]byte(ev.SessionID + "\x00" + ev.ToolUseID))
	return filepath.Join(root, hex.EncodeToString(sum[:]))
}

func claimReviewContext(ev *Event) (string, error) {
	root := filepath.Join(claudeDir(), inflightDirName)
	entry := reviewEntry(root, ev)
	claim, err := os.OpenFile(
		filepath.Join(entry, "context.claimed"),
		os.O_WRONLY|os.O_CREATE|os.O_EXCL,
		0o600,
	)
	if os.IsExist(err) || os.IsNotExist(err) {
		return "", nil
	}
	if err != nil {
		return "", err
	}
	_ = claim.Close()

	body, err := os.ReadFile(filepath.Join(entry, "result.json"))
	if err != nil {
		return "", err
	}
	var verdict sharedVerdict
	if err := json.Unmarshal(body, &verdict); err != nil {
		return "", err
	}
	if verdict.Decision == nil {
		return "", nil
	}
	return verdict.Decision.AdditionalContext, nil
}

func leadReview(entry string, review func() *Decision) coordinatedReview {
	if err := os.WriteFile(filepath.Join(entry, "pid"), []byte(strconv.Itoa(os.Getpid())), 0o600); err != nil {
		return primaryCoordination(entry, gateBroken(
			fmt.Sprintf("could not record the review coordinator (%v)", err),
		))
	}
	decision := review()
	body, err := json.Marshal(sharedVerdict{Decision: decision})
	if err != nil {
		return primaryCoordination(entry, gateBroken(
			fmt.Sprintf("could not encode the shared review verdict (%v)", err),
		))
	}
	tmp := filepath.Join(entry, "result.tmp")
	if err := os.WriteFile(tmp, body, 0o600); err != nil {
		return primaryCoordination(entry, gateBroken(
			fmt.Sprintf("could not record the shared review verdict (%v)", err),
		))
	}
	if err := os.Rename(tmp, filepath.Join(entry, "result.json")); err != nil {
		return primaryCoordination(entry, gateBroken(
			fmt.Sprintf("could not publish the shared review verdict (%v)", err),
		))
	}
	return primaryCoordination(entry, decision)
}

func awaitReview(ctx context.Context, entry string) coordinatedReview {
	started := time.Now()
	ticker := time.NewTicker(100 * time.Millisecond)
	defer ticker.Stop()
	var decision *Decision
	resultLoaded := false
	for {
		if !resultLoaded {
			body, err := os.ReadFile(filepath.Join(entry, "result.json"))
			if err == nil {
				var verdict sharedVerdict
				if err := json.Unmarshal(body, &verdict); err != nil {
					return primaryCoordination(entry, gateBroken(
						fmt.Sprintf("the shared review verdict was malformed (%v)", err),
					))
				}
				decision = verdict.Decision
				resultLoaded = true
			} else if !os.IsNotExist(err) {
				return primaryCoordination(entry, gateBroken(
					fmt.Sprintf("could not read the shared review verdict (%v)", err),
				))
			}
		}
		if resultLoaded {
			if _, err := os.Stat(filepath.Join(entry, "response.ready")); err == nil {
				return coordinatedReview{Decision: decision, Entry: entry}
			} else if !os.IsNotExist(err) {
				return primaryCoordination(entry, gateBroken(
					fmt.Sprintf("could not inspect the primary review response (%v)", err),
				))
			}
		}

		if time.Since(started) > time.Second {
			alive, err := reviewLeaderAlive(entry)
			if err != nil {
				return primaryCoordination(entry, gateBroken(
					fmt.Sprintf("could not inspect the duplicate review coordinator (%v)", err),
				))
			}
			if !alive {
				return primaryCoordination(entry, gateBroken(
					"the duplicate review coordinator exited without a complete response",
				))
			}
		}

		select {
		case <-ctx.Done():
			return primaryCoordination(entry, gateBroken(
				fmt.Sprintf("timed out waiting for the shared review verdict (%v)", ctx.Err()),
			))
		case <-ticker.C:
		}
	}
}

func markReviewResponded(entry string) error {
	if entry == "" {
		return nil
	}
	return os.WriteFile(filepath.Join(entry, "response.ready"), nil, 0o600)
}

func reviewLeaderAlive(entry string) (bool, error) {
	body, err := os.ReadFile(filepath.Join(entry, "pid"))
	if os.IsNotExist(err) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	pid, err := strconv.Atoi(string(body))
	if err != nil {
		return false, err
	}
	process, err := os.FindProcess(pid)
	if err != nil {
		return false, err
	}
	err = process.Signal(syscall.Signal(0))
	return err == nil || errors.Is(err, syscall.EPERM), nil
}

func pruneInflight(root string) {
	entries, err := os.ReadDir(root)
	if err != nil {
		return
	}
	cutoff := time.Now().Add(-staleFlagAge)
	for _, entry := range entries {
		info, err := entry.Info()
		if err == nil && info.ModTime().Before(cutoff) {
			_ = os.RemoveAll(filepath.Join(root, entry.Name()))
		}
	}
}
