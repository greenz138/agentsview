package parser

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/tidwall/gjson"
)

func TestDiscoverCodeBuddySessions(t *testing.T) {
	root := t.TempDir()
	historyDir := filepath.Join(root, "history", "ws_123")

	// Workspace index (should NOT be classified as a session)
	require.NoError(t, os.MkdirAll(historyDir, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(historyDir, "index.json"), []byte(`{"conversations":[]}`), 0o644))

	// Session 1
	s1Dir := filepath.Join(historyDir, "conv_abc")
	require.NoError(t, os.MkdirAll(filepath.Join(s1Dir, "messages"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(s1Dir, "index.json"), []byte(`{"messages":[]}`), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(s1Dir, "messages", "m1.json"), []byte(`{}`), 0o644))

	// Session 2
	s2Dir := filepath.Join(historyDir, "conv_def")
	require.NoError(t, os.MkdirAll(s2Dir, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(s2Dir, "index.json"), []byte(`{"messages":[]}`), 0o644))

	// Non-matching file
	require.NoError(t, os.WriteFile(filepath.Join(historyDir, "other.json"), []byte(`{}`), 0o644))

	sourceSet := newCodeBuddySourceSet([]string{root})
	discovered, err := sourceSet.Discover(t.Context())
	require.NoError(t, err)
	require.Len(t, discovered, 2)

	assert.Equal(t, filepath.Join(s1Dir, "index.json"), discovered[0].DisplayPath)
	assert.Equal(t, "ws_123", discovered[0].ProjectHint)

	assert.Equal(t, filepath.Join(s2Dir, "index.json"), discovered[1].DisplayPath)
	assert.Equal(t, "ws_123", discovered[1].ProjectHint)
}

func TestParseCodeBuddySession(t *testing.T) {
	root := t.TempDir()
	historyDir := filepath.Join(root, "history", "ws_test")
	require.NoError(t, os.MkdirAll(historyDir, 0o755))

	// Workspace index with title and model metadata
	wsIndexContent := `{
  "conversations": [
    {
      "id": "conv_001",
      "name": "Build Go Parser",
      "createdAt": "2026-09-17T02:00:00.000Z",
      "selectedModelId": "deepseek-v4.1-flash"
    }
  ]
}`
	require.NoError(t, os.WriteFile(filepath.Join(historyDir, "index.json"), []byte(wsIndexContent), 0o644))

	sessionDir := filepath.Join(historyDir, "conv_001")
	messagesDir := filepath.Join(sessionDir, "messages")
	require.NoError(t, os.MkdirAll(messagesDir, 0o755))

	sessionIndexContent := `{
  "messages": [
    {"id": "msg_u1", "role": "user", "type": "text"},
    {"id": "msg_a1", "role": "assistant", "type": "text"},
    {"id": "msg_t1", "role": "tool", "type": "text"},
    {"id": "msg_a2", "role": "assistant", "type": "text"}
  ]
}`
	indexPath := filepath.Join(sessionDir, "index.json")
	require.NoError(t, os.WriteFile(indexPath, []byte(sessionIndexContent), 0o644))

	// User message
	userMsgContent := `{
  "id": "msg_u1",
  "role": "user",
  "createdAt": "2026-09-17T02:00:01.000Z",
  "message": "{\"role\":\"user\",\"content\":[{\"type\":\"text\",\"text\":\"<user_info>\\nWorkspace Folder: /workspace/demo\\n</user_info>\\n<user_query>\\nPlease inspect the code.\\n</user_query>\"}]}",
  "extra": "{}"
}`
	require.NoError(t, os.WriteFile(filepath.Join(messagesDir, "msg_u1.json"), []byte(userMsgContent), 0o644))

	// Assistant message with tool call
	asstMsg1Content := `{
  "id": "msg_a1",
  "role": "assistant",
  "createdAt": "2026-09-17T02:00:05.000Z",
  "message": "{\"role\":\"assistant\",\"content\":[{\"type\":\"text\",\"text\":\"Let me check.\"},{\"type\":\"tool-call\",\"toolCallId\":\"call_999\",\"toolName\":\"execute_command\",\"args\":{\"command\":\"ls -la\"}}]}",
  "extra": "{\"modelId\":\"deepseek-v4.1-flash\",\"lastStepInputTokens\":100,\"lastStepOutputTokens\":25,\"lastStepCachedInputTokens\":80}"
}`
	require.NoError(t, os.WriteFile(filepath.Join(messagesDir, "msg_a1.json"), []byte(asstMsg1Content), 0o644))

	// Tool result message
	toolMsgContent := `{
  "id": "msg_t1",
  "role": "tool",
  "createdAt": "2026-09-17T02:00:06.000Z",
  "message": "{\"role\":\"tool\",\"content\":[{\"type\":\"tool-result\",\"toolCallId\":\"call_999\",\"result\":{\"result\":{\"stdout\":\"file1.txt\\nfile2.txt\"}}}]}",
  "extra": "{}"
}`
	require.NoError(t, os.WriteFile(filepath.Join(messagesDir, "msg_t1.json"), []byte(toolMsgContent), 0o644))

	// Final assistant message
	asstMsg2Content := `{
  "id": "msg_a2",
  "role": "assistant",
  "createdAt": "2026-09-17T02:00:10.000Z",
  "message": "{\"role\":\"assistant\",\"content\":[{\"type\":\"text\",\"text\":\"All files inspected successfully.\"}]}",
  "extra": "{\"modelId\":\"deepseek-v4.1-flash\",\"lastStepInputTokens\":150,\"lastStepOutputTokens\":30,\"lastStepCachedInputTokens\":100}"
}`
	require.NoError(t, os.WriteFile(filepath.Join(messagesDir, "msg_a2.json"), []byte(asstMsg2Content), 0o644))

	sess, msgs, err := parseCodeBuddySession(indexPath, "", "local")
	require.NoError(t, err)
	require.NotNil(t, sess)

	assert.Equal(t, "codebuddy:conv_001", sess.ID)
	assert.Equal(t, "Build Go Parser", sess.SessionName)
	assert.Equal(t, "Please inspect the code.", sess.FirstMessage)
	assert.Equal(t, "/workspace/demo", sess.Cwd)
	assert.Equal(t, 1, sess.UserMessageCount)
	assert.Len(t, msgs, 4)

	// Verify User Message
	assert.Equal(t, RoleUser, msgs[0].Role)
	assert.Equal(t, "Please inspect the code.", msgs[0].Content)

	// Verify Assistant Tool Call
	assert.Equal(t, RoleAssistant, msgs[1].Role)
	assert.True(t, msgs[1].HasToolUse)
	require.Len(t, msgs[1].ToolCalls, 1)
	assert.Equal(t, "call_999", msgs[1].ToolCalls[0].ToolUseID)
	assert.Equal(t, "execute_command", msgs[1].ToolCalls[0].ToolName)
	assert.Equal(t, `{"command":"ls -la"}`, msgs[1].ToolCalls[0].InputJSON)
	assert.Equal(t, "deepseek-v4.1-flash", msgs[1].Model)

	// Verify Token Usage calculation (input = 100 - 80 = 20, cached = 80, output = 25)
	assert.Equal(t, 25, msgs[1].OutputTokens)
	assert.Equal(t, 100, msgs[1].ContextTokens)
	assert.Equal(t, int64(20), gjson.GetBytes(msgs[1].TokenUsage, "input_tokens").Int())
	assert.Equal(t, int64(80), gjson.GetBytes(msgs[1].TokenUsage, "cache_read_input_tokens").Int())
	assert.Equal(t, int64(25), gjson.GetBytes(msgs[1].TokenUsage, "output_tokens").Int())

	// Verify Tool Result
	assert.Equal(t, RoleUser, msgs[2].Role)
	require.Len(t, msgs[2].ToolResults, 1)
	assert.Equal(t, "call_999", msgs[2].ToolResults[0].ToolUseID)
	assert.Equal(t, "\"file1.txt\\nfile2.txt\"", msgs[2].ToolResults[0].ContentRaw)

	// Verify Final Assistant Message
	assert.Equal(t, RoleAssistant, msgs[3].Role)
	assert.Equal(t, "All files inspected successfully.", msgs[3].Content)
}

func TestCodeBuddyPathHelpers(t *testing.T) {
	validPath := filepath.Join("C:", "data", "history", "ws_123", "conv_abc", "index.json")
	assert.True(t, isCodeBuddySourcePath("C:\\data", validPath))

	// Invalid: not index.json
	assert.False(t, isCodeBuddySourcePath("C:\\data", filepath.Join("C:", "data", "history", "ws_123", "conv_abc", "other.json")))

	// Invalid: workspace level index.json
	assert.False(t, isCodeBuddySourcePath("C:\\data", filepath.Join("C:", "data", "history", "ws_123", "index.json")))

	// Project hint & session ID
	assert.Equal(t, "ws_123", codeBuddyProjectHintFromPath("C:\\data", validPath))
	assert.Equal(t, "conv_abc", codeBuddySessionIDFromPath("C:\\data", validPath))

	// Lookup ID
	assert.True(t, isCodeBuddyLookupID("codebuddy:conv_abc"))
	assert.True(t, isCodeBuddyLookupID("conv_abc"))
	assert.False(t, isCodeBuddyLookupID("codebuddy:invalid/slash"))
}

func TestCodeBuddyCompanionFiles(t *testing.T) {
	root := t.TempDir()
	historyDir := filepath.Join(root, "history", "ws_test")
	sessionDir := filepath.Join(historyDir, "conv_001")
	messagesDir := filepath.Join(sessionDir, "messages")
	require.NoError(t, os.MkdirAll(messagesDir, 0o755))

	wsIndex := filepath.Join(historyDir, "index.json")
	require.NoError(t, os.WriteFile(wsIndex, []byte(`{}`), 0o644))

	indexPath := filepath.Join(sessionDir, "index.json")
	require.NoError(t, os.WriteFile(indexPath, []byte(`{}`), 0o644))

	msg1 := filepath.Join(messagesDir, "m1.json")
	require.NoError(t, os.WriteFile(msg1, []byte(`{}`), 0o644))

	companions := codeBuddyCompanionFiles(indexPath)
	assert.Contains(t, companions, wsIndex)
	assert.Contains(t, companions, msg1)

	// Companion transcript mapping
	transcript, ok := codeBuddyCompanionTranscript(msg1)
	assert.True(t, ok)
	assert.Equal(t, indexPath, transcript)

	// Non-message file
	_, ok = codeBuddyCompanionTranscript(wsIndex)
	assert.False(t, ok)
}
