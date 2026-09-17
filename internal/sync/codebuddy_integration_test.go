package sync

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.kenn.io/agentsview/internal/parser"
)

func TestSyncCodeBuddySameSizeSameMtimeRewrite(t *testing.T) {
	for _, warmCache := range []bool{false, true} {
		for _, target := range []string{"message", "workspace", "usage"} {
			t.Run(fmt.Sprintf("%s/warm=%t", target, warmCache), func(t *testing.T) {
				root := t.TempDir()
				workspace := filepath.Join(root, "history", "ws_test")
				dir := filepath.Join(workspace, "conv_test")
				require.NoError(t, os.MkdirAll(filepath.Join(dir, "messages"), 0o755))
				files := map[string]string{
					filepath.Join(dir, "index.json"):          `{"messages":[{"id":"u1"},{"id":"a1"}]}`,
					filepath.Join(workspace, "index.json"):    `{"conversations":[{"id":"conv_test","name":"before"}]}`,
					filepath.Join(dir, "messages", "u1.json"): `{"role":"user","message":{"content":[{"type":"text","text":"hello"}]}}`,
					filepath.Join(dir, "messages", "a1.json"): `{"role":"assistant","message":{"content":[{"type":"text","text":"reply"}]},"extra":{"lastStepOutputTokens":10}}`,
				}
				for path, content := range files {
					require.NoError(t, os.WriteFile(path, []byte(content), 0o600))
				}
				database := openTestDB(t)
				engine := NewEngine(database, EngineConfig{AgentDirs: map[parser.AgentType][]string{parser.AgentCodeBuddy: {root}}, Machine: "test"})
				t.Cleanup(engine.Close)
				require.Equal(t, 1, engine.SyncAll(t.Context(), nil).Synced)
				if warmCache {
					engine.SyncAll(t.Context(), nil)
				}
				path := filepath.Join(dir, "messages", "u1.json")
				replacement := `{"role":"user","message":{"content":[{"type":"text","text":"howdy"}]}}`
				switch target {
				case "workspace":
					path = filepath.Join(workspace, "index.json")
					replacement = `{"conversations":[{"id":"conv_test","name":"after!"}]}`
				case "usage":
					path = filepath.Join(dir, "messages", "a1.json")
					replacement = `{"role":"assistant","message":{"content":[{"type":"text","text":"reply"}]},"extra":{"lastStepOutputTokens":20}}`
				}
				before, err := os.Stat(path)
				require.NoError(t, err)
				require.EqualValues(t, before.Size(), len(replacement))
				require.NoError(t, os.WriteFile(path, []byte(replacement), 0o600))
				require.NoError(t, os.Chtimes(path, before.ModTime(), before.ModTime()))
				after, err := os.Stat(path)
				require.NoError(t, err)
				require.Equal(t, before.ModTime(), after.ModTime())
				engine.SyncAll(t.Context(), nil)
				sess, err := database.GetSessionFull(t.Context(), "codebuddy:conv_test")
				require.NoError(t, err)
				require.NotNil(t, sess)
				msgs, err := database.GetMessages(t.Context(), "codebuddy:conv_test", 0, 100, true)
				require.NoError(t, err)
				require.Len(t, msgs, 2)
				switch target {
				case "message":
					assert.Equal(t, "howdy", msgs[0].Content)
				case "workspace":
					require.NotNil(t, sess.SessionName)
					assert.Equal(t, "after!", *sess.SessionName)
				case "usage":
					assert.Equal(t, 20, msgs[1].OutputTokens)
				}
			})
		}
	}
}
