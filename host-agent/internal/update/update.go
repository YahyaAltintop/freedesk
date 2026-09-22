// Package update asks GitHub whether a newer release of FreeDesk exists.
//
// It is a courtesy, not a mechanism: the agent never downloads or replaces
// anything, it only tells the operator where the new version is. The check
// runs in the background and is allowed to fail in every way — no network, a
// 403 from GitHub's rate limit, an odd answer — without a word to the
// operator, because nothing they need depends on it.
package update

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"
)

const (
	apiBase = "https://api.github.com"

	// timeout bounds the whole request. Generous, because it costs nothing:
	// the check runs beside the start-up, never in front of it.
	timeout = 10 * time.Second

	// maxBody is more than a release description ever needs.
	maxBody = 1 << 20
)

// Release is the newest published release.
type Release struct {
	Version string // "0.4.0", without the tag's leading v
	URL     string // the page to download it from
}

// Check returns the latest release when it is newer than current. ok is false
// when there is nothing newer or the answer could not be had; err says why, for
// a caller that wants to know (a test, a log) — the agent itself does not.
func Check(ctx context.Context, repo, current string) (latest Release, ok bool, err error) {
	return check(ctx, apiBase, repo, current)
}

func check(ctx context.Context, base, repo, current string) (Release, bool, error) {
	if repo == "" {
		return Release{}, false, errors.New("no repository configured")
	}
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	// The "latest" endpoint answers with the newest release that is neither a
	// draft nor a pre-release, which is exactly what an operator should get.
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, base+"/repos/"+repo+"/releases/latest", nil)
	if err != nil {
		return Release{}, false, err
	}
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("User-Agent", "FreeDesk/"+current) // GitHub refuses requests without one
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return Release{}, false, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		// 403 or 429: the address's share of the unauthenticated rate limit
		// (60 requests an hour) is used up. 404: nothing published yet.
		return Release{}, false, fmt.Errorf("GitHub answered HTTP %d", resp.StatusCode)
	}

	var body struct {
		TagName string `json:"tag_name"`
	}
	if err := json.NewDecoder(io.LimitReader(resp.Body, maxBody)).Decode(&body); err != nil {
		return Release{}, false, fmt.Errorf("unreadable answer from GitHub: %w", err)
	}
	version := strings.TrimPrefix(strings.TrimSpace(body.TagName), "v")
	if version == "" {
		return Release{}, false, errors.New("the release has no tag")
	}
	if compareVersions(version, current) <= 0 {
		return Release{}, false, nil
	}
	// The page is built from the configured repository rather than taken from
	// the answer, so nothing GitHub says decides where a click goes.
	return Release{Version: version, URL: "https://github.com/" + repo + "/releases/latest"}, true, nil
}

// compareVersions orders "1.2.3"-style versions numerically: negative when a
// is older than b, zero when equal, positive when newer. A leading v and
// anything from a hyphen or plus on ("-dev", "-rc1", "+build") are ignored, so
// a development build counts as the release it is heading for. Missing parts
// are zero: "1.2" is "1.2.0".
func compareVersions(a, b string) int {
	pa, pb := versionParts(a), versionParts(b)
	for i := range max(len(pa), len(pb)) {
		var x, y int
		if i < len(pa) {
			x = pa[i]
		}
		if i < len(pb) {
			y = pb[i]
		}
		if x != y {
			if x < y {
				return -1
			}
			return 1
		}
	}
	return 0
}

func versionParts(v string) []int {
	v = strings.TrimPrefix(strings.TrimSpace(v), "v")
	if i := strings.IndexAny(v, "-+"); i >= 0 {
		v = v[:i]
	}
	var parts []int
	for p := range strings.SplitSeq(v, ".") {
		n, err := strconv.Atoi(p)
		if err != nil || n < 0 {
			n = 0
		}
		parts = append(parts, n)
	}
	return parts
}
