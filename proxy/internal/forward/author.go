package forward

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
)

// Author is a git identity written into a commit.
type Author struct {
	Name  string `json:"name"`
	Email string `json:"email"`
}

func (a Author) valid() bool { return a.Name != "" && a.Email != "" }

func (a Author) String() string { return fmt.Sprintf("%s <%s>", a.Name, a.Email) }

// InjectAuthor returns body with "author" set to editor and "committer" set to
// committer, discarding whatever the browser sent for either. A nil or empty
// body yields an object holding just those two fields. Non-object JSON is an
// error.
//
// Decap sends neither field on a normal save: commit() calls createCommit with
// three arguments, so author and committer are undefined and JSON.stringify
// drops them. The wire body is exactly {"message","tree","parents"}. So this is
// injection, not overwriting — but it must also overwrite, because
// rebaseSingleCommit does send both, copied off the commits it is replaying.
//
// GitHub defaults the committer to the author when only the author is given, so
// setting the committer explicitly is what keeps the bot named as committer and
// the editor as author.
//
// Every other key is preserved byte for byte.
func InjectAuthor(body []byte, editor, committer Author) ([]byte, error) {
	if !editor.valid() {
		return nil, errors.New("forward: editor identity needs both a name and an email")
	}
	if !committer.valid() {
		return nil, errors.New("forward: committer identity needs both a name and an email")
	}

	fields := map[string]json.RawMessage{}
	if trimmed := bytes.TrimSpace(body); len(trimmed) > 0 && !bytes.Equal(trimmed, []byte("null")) {
		if err := json.Unmarshal(trimmed, &fields); err != nil {
			return nil, fmt.Errorf("forward: request body is not a JSON object: %w", err)
		}
	}

	encoded, err := json.Marshal(editor)
	if err != nil {
		return nil, err
	}
	fields["author"] = encoded

	encoded, err = json.Marshal(committer)
	if err != nil {
		return nil, err
	}
	fields["committer"] = encoded

	return json.Marshal(fields)
}
