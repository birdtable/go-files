package trust

import (
	"errors"
	"testing"
)

// wantPath fails t unless err is an *Unsafe whose Path is want.
func wantPath(t *testing.T, name string, err error, want string) {
	t.Helper()
	u, ok := errors.AsType[*Unsafe](err)
	switch {
	case !ok:
		t.Errorf("%s: %v, and want an *Unsafe", name, err)
	case u.Path != want:
		t.Errorf("%s: Unsafe.Path is %q, and want %q (%s)", name, u.Path, want, u.Observed)
	}
}

// What it costs when this fails: a caller that names its own subject first cannot tell whether
// the walk judged that name or a folder on its way, and says the name twice, or reads the words.
func TestEachRefusalNamesTheNameItJudgedInUnsafePath(t *testing.T) {
	t.Parallel()
	o, f, l := at("o"), at("o", "f"), at("l")
	cases := []struct {
		name  string
		nodes map[string]node
		path  string
		who   Who
		want  string
	}{
		{"the last name", map[string]node{o: dir(meID, 0o755), f: file(meID, 0o666)}, f, Who{}, f},
		{"a folder on the way", map[string]node{o: dir(meID, 0o777), f: file(meID, 0o644)}, f, Who{}, o},
		{"a folder of another's on the way", map[string]node{o: dir(otherID, 0o755), f: file(meID, 0o644)}, f, Who{}, o},
		{"a link on the way, no link followed", map[string]node{l: link(meID)}, at("l", "f"), Who{NoLinks: true}, l},
		{"a path that is not absolute", nil, "f", Who{}, "f"},
		{"a setting of Who, which names no path", map[string]node{o: dir(meID, 0o755), f: file(meID, 0o644)}, f, Who{Apart: []string{"o"}}, ""},
	}
	for _, c := range cases {
		_, err := walk(c.path, File, c.who, disk(c.nodes).host(meID))
		wantPath(t, c.name, err, c.want)
	}

	h := disk(map[string]node{f: file(otherID, 0o644)}).host(meID)
	_, err := readOwned(f, meID, false, 0, h, func() {})
	wantPath(t, "ReadOwned with no limit", err, f)
	wantPath(t, "ReadOwned of a file another owns", judgeRead(file(otherID, 0o644), f, meID, false, 10, h), f)
	wantPath(t, "ReadOwned of a private file others read", judgeRead(file(meID, 0o644), f, meID, true, 10, h), f)
}

// What it costs when this fails: a path in a folder of Who.Apart that is spelled through a link
// is refused naming the link, a name the walk never met, in place of the folder on its way.
func TestARefusalOfAnApartFolderNamesTheNameOnTheWayNotTheSpellingOfWhoApart(t *testing.T) {
	t.Parallel()
	o, f, lo := at("o"), at("o", "f"), at("lo")
	d := disk(map[string]node{o: the(dir(meID, 0o755), 7), f: the(file(meID, 0o644), 8)})
	var reads int
	_, err := walk(f, File, Who{Apart: []string{lo}}, apartHost(d, map[string]uint64{lo: 7}, &reads))
	wantPath(t, "a file in an apart folder spelled by a link", err, o)
}
