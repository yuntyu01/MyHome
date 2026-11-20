package run

import (
	"bufio"
	"errors"
	"fmt"
	"os"
	"strconv"
	"strings"

	"github.com/yuntyu01/MyHome/server/internal/invest"
)

// readMasterCSV는 테스트 가격 마스터(testdata의 date,SPX,NDX,… CSV)를 읽는다. 빈 칸·nan은 결측이다.
func readMasterCSV(path string) (invest.Prices, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	sc := bufio.NewScanner(f)
	var cols []string
	var out invest.Prices
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		fs := strings.Split(line, ",")
		if cols == nil {
			cols = fs
			if cols[0] != "date" {
				return nil, fmt.Errorf("첫 열은 date여야 한다: %v", cols)
			}
			continue
		}
		if len(fs) != len(cols) {
			return nil, fmt.Errorf("열 수가 다르다: %q", line)
		}
		d, err := invest.ParseDay(fs[0])
		if err != nil {
			return nil, fmt.Errorf("날짜 형식 이상: %q", fs[0])
		}
		r := invest.EmptyRow(d)
		for i, c := range cols[1:] {
			s := strings.TrimSpace(fs[i+1])
			if s == "" || s == "nan" || s == "NaN" {
				continue
			}
			v, err := strconv.ParseFloat(s, 64)
			if err != nil {
				return nil, fmt.Errorf("%s %s 숫자 아님: %q", fs[0], c, s)
			}
			r.Set(c, v)
		}
		if len(out) > 0 && !d.After(out[len(out)-1].Day) {
			return nil, fmt.Errorf("날짜가 오름차순이 아니거나 중복: %s", fs[0])
		}
		out = append(out, r)
	}
	if err := sc.Err(); err != nil {
		return nil, err
	}
	if len(out) == 0 {
		return nil, errors.New("행이 없다")
	}
	return out, nil
}
