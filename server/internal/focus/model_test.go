package focus

import (
	"strings"
	"testing"
	"time"
)

func TestValidID(t *testing.T) {
	for _, ok := range []string{"3f2b8c1e-9a4d-4e7b-8c1f-2a3b4c5d6e7f", "a", "ABC-123", strings.Repeat("x", 64)} {
		if !ValidID(ok) {
			t.Errorf("ValidID(%q) = false", ok)
		}
	}
	for _, bad := range []string{"", strings.Repeat("x", 65), "a_b", "a b", "../x", "수학", "a/b", "a.b", "a\n"} {
		if ValidID(bad) {
			t.Errorf("ValidID(%q) = true", bad)
		}
	}
}

var updated = time.Date(2026, 11, 9, 12, 0, 0, 0, time.UTC)

func TestSubjectNormalize(t *testing.T) {
	in := SubjectInput{Name: "  수학  ", Color: "#ff8800", Sort: 3, UpdatedAt: updated}
	if err := in.normalize(); err != nil {
		t.Fatal(err)
	}
	if in.Name != "수학" || in.Color != "#FF8800" {
		t.Errorf("normalize = %q %q", in.Name, in.Color)
	}
	// 한글 40자는 된다(바이트가 아니라 글자 수)
	if in := (SubjectInput{Name: strings.Repeat("가", 40), Color: "#000000", UpdatedAt: updated}); in.normalize() != nil {
		t.Error("40자는 돼야 함")
	}

	for name, in := range map[string]SubjectInput{
		"이름 없음":        {Name: "   ", Color: "#000000", UpdatedAt: updated},
		"41자":          {Name: strings.Repeat("가", 41), Color: "#000000", UpdatedAt: updated},
		"줄바꿈":          {Name: "수\n학", Color: "#000000", UpdatedAt: updated},
		"NUL":          {Name: "수\x00학", Color: "#000000", UpdatedAt: updated},
		"색 # 없음":       {Name: "수학", Color: "000000", UpdatedAt: updated},
		"색 5자리":        {Name: "수학", Color: "#00000", UpdatedAt: updated},
		"색 알파":         {Name: "수학", Color: "#FF000000", UpdatedAt: updated},
		"색 16진 아님":     {Name: "수학", Color: "#GGGGGG", UpdatedAt: updated},
		"색 이름":         {Name: "수학", Color: "red", UpdatedAt: updated},
		"updatedAt 없음": {Name: "수학", Color: "#000000"},
	} {
		if err := in.normalize(); codeOf(err) != CodeInvalid {
			t.Errorf("%s: %v, want INVALID", name, err)
		}
	}
}

func TestSessionNormalize(t *testing.T) {
	s := time.Date(2026, 11, 10, 4, 0, 0, 0, time.UTC)
	ok := SessionInput{SubjectID: "math", StartedAt: s, EndedAt: s.Add(time.Hour), UpdatedAt: updated}
	if err := ok.normalize(); err != nil {
		t.Fatal(err)
	}
	// 딱 24시간은 된다
	full := ok
	full.EndedAt = s.Add(MaxDuration)
	if err := full.normalize(); err != nil {
		t.Errorf("24시간: %v", err)
	}

	cases := []struct {
		name string
		edit func(*SessionInput)
		code string
	}{
		{"과목 없음", func(in *SessionInput) { in.SubjectID = "" }, CodeInvalid},
		{"과목 id 형식", func(in *SessionInput) { in.SubjectID = "수학" }, CodeInvalid},
		{"시작 없음", func(in *SessionInput) { in.StartedAt = time.Time{} }, CodeInvalid},
		{"끝 없음", func(in *SessionInput) { in.EndedAt = time.Time{} }, CodeInvalid},
		{"끝 = 시작", func(in *SessionInput) { in.EndedAt = in.StartedAt }, CodeInvalidRange},
		{"끝 < 시작", func(in *SessionInput) { in.EndedAt = in.StartedAt.Add(-time.Minute) }, CodeInvalidRange},
		{"24시간 초과", func(in *SessionInput) { in.EndedAt = in.StartedAt.Add(MaxDuration + time.Second) }, CodeTooLong},
		{"updatedAt 없음", func(in *SessionInput) { in.UpdatedAt = time.Time{} }, CodeInvalid},
	}
	for _, c := range cases {
		in := ok
		c.edit(&in)
		if err := in.normalize(); codeOf(err) != c.code {
			t.Errorf("%s: %v, want %s", c.name, err, c.code)
		}
	}
}

func TestStayNormalize(t *testing.T) {
	s := time.Date(2026, 11, 10, 0, 0, 0, 0, time.UTC)
	ok := StayInput{Place: " 시립도서관 ", EnteredAt: s, ExitedAt: s.Add(3 * time.Hour), UpdatedAt: updated}
	in := ok
	if err := in.normalize(); err != nil || in.Place != "시립도서관" {
		t.Fatalf("normalize = %q %v", in.Place, err)
	}

	cases := []struct {
		name string
		edit func(*StayInput)
		code string
	}{
		{"장소 없음", func(in *StayInput) { in.Place = " " }, CodeInvalid},
		{"장소 41자", func(in *StayInput) { in.Place = strings.Repeat("a", 41) }, CodeInvalid},
		{"들어간 시각 없음", func(in *StayInput) { in.EnteredAt = time.Time{} }, CodeInvalid},
		{"나온 시각 없음(아직 머무는 중)", func(in *StayInput) { in.ExitedAt = time.Time{} }, CodeInvalid},
		{"나옴 = 들어감", func(in *StayInput) { in.ExitedAt = in.EnteredAt }, CodeInvalidRange},
		{"24시간 초과", func(in *StayInput) { in.ExitedAt = in.EnteredAt.Add(25 * time.Hour) }, CodeTooLong},
		{"updatedAt 없음", func(in *StayInput) { in.UpdatedAt = time.Time{} }, CodeInvalid},
	}
	for _, c := range cases {
		in := ok
		c.edit(&in)
		if err := in.normalize(); codeOf(err) != c.code {
			t.Errorf("%s: %v, want %s", c.name, err, c.code)
		}
	}
}
