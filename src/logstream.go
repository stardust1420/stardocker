package src

import (
	"bufio"
	"context"
	"io"
	"sync/atomic"

	tea "github.com/charmbracelet/bubbletea"
	containerTypes "github.com/docker/docker/api/types/container"
	"github.com/docker/docker/pkg/stdcopy"
	"github.com/docker/go-sdk/client"
)

// The logs view loads the chosen time range once, then follows new output
// over a single streaming request instead of refetching everything. The
// stream ends when the container stops; the view starts a new one, from the
// last line it has, when the container runs again.

// logStream is one following ContainerLogs request.
type logStream struct {
	id     int
	cancel context.CancelFunc
	ch     chan string // lines, closed when the stream ends
}

// logLinesMsg delivers lines read from a stream. ended means the stream is
// finished (container stopped, error, or cancelled).
type logLinesMsg struct {
	streamID int
	lines    []string
	ended    bool
}

var lastStreamID atomic.Int64

// maxLinesPerMsg bounds how many lines one message carries, so a burst of
// output is drawn in a few steps instead of one huge one.
const maxLinesPerMsg = 500

// startLogStream follows the container's output from since. Lines keep
// their trailing newline.
func startLogStream(dockerClient client.SDKClient, containerID string, tty bool, since string) *logStream {
	ctx, cancel := context.WithCancel(context.Background())
	s := &logStream{id: int(lastStreamID.Add(1)), cancel: cancel, ch: make(chan string, 256)}

	go func() {
		defer close(s.ch)
		rc, err := dockerClient.ContainerLogs(ctx, containerID, containerTypes.LogsOptions{
			ShowStdout: true, ShowStderr: true, Follow: true, Timestamps: true, Since: since,
		})
		if err != nil {
			return
		}
		defer rc.Close()

		// Without a TTY the stream is multiplexed; demux it through a pipe so
		// it can be read line by line.
		var r io.Reader = rc
		if !tty {
			pr, pw := io.Pipe()
			go func() {
				_, err := stdcopy.StdCopy(pw, pw, rc)
				pw.CloseWithError(err)
			}()
			defer pr.Close()
			r = pr
		}

		br := bufio.NewReaderSize(r, 64*1024)
		for {
			line, err := br.ReadString('\n')
			if line != "" {
				select {
				case s.ch <- line:
				case <-ctx.Done():
					return
				}
			}
			if err != nil {
				return
			}
		}
	}()
	return s
}

// stop ends the stream; its pending wait returns an ended message.
func (s *logStream) stop() {
	if s != nil {
		s.cancel()
	}
}

// waitLogs waits for the next lines of the stream, then takes whatever else
// is already buffered.
func waitLogs(s *logStream) tea.Cmd {
	if s == nil {
		return nil
	}
	return func() tea.Msg {
		first, ok := <-s.ch
		if !ok {
			return logLinesMsg{streamID: s.id, ended: true}
		}
		lines := []string{first}
		for len(lines) < maxLinesPerMsg {
			select {
			case line, ok := <-s.ch:
				if !ok {
					return logLinesMsg{streamID: s.id, lines: lines, ended: true}
				}
				lines = append(lines, line)
			default:
				return logLinesMsg{streamID: s.id, lines: lines}
			}
		}
		return logLinesMsg{streamID: s.id, lines: lines}
	}
}

// maxLogBytes caps the log text kept in memory; the oldest lines go first.
const maxLogBytes = 8 << 20

// appendCapped appends text and trims whole lines from the front past the
// cap.
func appendCapped(logs, text string) string {
	logs += text
	if len(logs) <= maxLogBytes {
		return logs
	}
	cut := len(logs) - maxLogBytes
	for cut < len(logs) && logs[cut-1] != '\n' {
		cut++
	}
	return logs[cut:]
}
