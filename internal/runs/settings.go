package runs

import (
	"context"
	"errors"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
)

// Settings is the operator-editable configuration.
type Settings struct {
	DevtronURL string    `json:"devtronUrl"`
	UpdatedAt  time.Time `json:"updatedAt"`
	UpdatedBy  string    `json:"updatedBy,omitempty"`
	// Token is never serialised. The API reports only whether one is set and
	// its last four characters.
	Token string `json:"-"`
}

// LoadSettings returns the stored settings, or false when none were saved and
// the environment is still authoritative.
func (s *Store) LoadSettings(ctx context.Context) (*Settings, bool, error) {
	var out Settings
	err := s.pool.QueryRow(ctx,
		`select devtron_url, devtron_token, updated_at, updated_by from settings where id = true`).
		Scan(&out.DevtronURL, &out.Token, &out.UpdatedAt, &out.UpdatedBy)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, false, nil
	}
	if err != nil {
		return nil, false, err
	}
	return &out, true, nil
}

// SaveSettings upserts the single settings row. An empty token means "keep
// the one already stored", so an operator can change the host without having
// to paste the credential again.
func (s *Store) SaveSettings(ctx context.Context, url, token, by string) (*Settings, error) {
	url = strings.TrimRight(strings.TrimSpace(url), "/")
	if url == "" {
		return nil, errors.New("a Devtron URL is required")
	}
	if !strings.HasPrefix(url, "http://") && !strings.HasPrefix(url, "https://") {
		return nil, errors.New("the Devtron URL must start with http:// or https://")
	}
	_, err := s.pool.Exec(ctx, `
		insert into settings (id, devtron_url, devtron_token, updated_at, updated_by)
		values (true, $1, $2, now(), $3)
		on conflict (id) do update set
			devtron_url   = excluded.devtron_url,
			devtron_token = case when excluded.devtron_token = '' then settings.devtron_token
			                     else excluded.devtron_token end,
			updated_at    = now(),
			updated_by    = excluded.updated_by`,
		url, strings.TrimSpace(token), by)
	if err != nil {
		return nil, err
	}
	got, _, err := s.LoadSettings(ctx)
	return got, err
}
