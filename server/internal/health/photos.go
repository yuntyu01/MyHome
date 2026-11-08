package health

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"

	"github.com/jackc/pgx/v5"

	"github.com/yuntyu01/MyHome/server/internal/dbq"
)

// 끼니 사진 서버 보관 (2026-11-08). 파일은 PhotoDir(MEAL_PHOTO_DIR) 아래 날짜 폴더에, DB(meal_photos)에는 위치만 둔다.
// 폰은 기록을 저장한 뒤 그 끼니 사진을 통째로 올리고(PUT, 바꾸기), 폰에서 지워진 사진은 다시 받아 간다(GET).
// 파일 이름에 내용 해시를 넣어서, 같은 사진을 다시 올리면 파일을 새로 쓰지 않는다.

// MaxMealPhotos는 끼니 하나에 남기는 사진 수다(분석과 같다).
const MaxMealPhotos = 3

// MealPhotoView는 사진 목록의 한 장이다. 내용은 GET /v1/health/meals/photos/{date}/{index}/{n}로 받는다.
type MealPhotoView struct {
	N      int    `json:"n"`
	Bytes  int    `json:"bytes"`
	Sha256 string `json:"sha256"`
}

func photoView(p dbq.MealPhoto) MealPhotoView {
	return MealPhotoView{N: int(p.N), Bytes: int(p.Bytes), Sha256: hex.EncodeToString(p.Sha256)}
}

func (s *Service) photoDir() (string, error) {
	if s.PhotoDir == "" {
		return "", &Error{http.StatusServiceUnavailable, "PHOTO_STORAGE_OFF", "서버에 사진 저장이 꺼져 있어요(MEAL_PHOTO_DIR)"}
	}
	return s.PhotoDir, nil
}

func checkMealIndex(index int) error {
	if index < 1 || index > 9 {
		return invalid("index", "1~9여야 해요")
	}
	return nil
}

// isJPEG는 JPEG 시작 표식(FF D8 FF)을 본다. 폰은 늘 JPEG로 줄여 보낸다(MealPhotos.downscaled).
func isJPEG(b []byte) bool { return len(b) > 3 && b[0] == 0xFF && b[1] == 0xD8 && b[2] == 0xFF }

// ReplaceMealPhotos는 그 끼니의 사진을 photos로 바꾼다(0장이면 지우기만). 끼니 기록이 없으면 404다.
// 끼니 행을 잠근(SELECT ... FOR UPDATE) 트랜잭션 안에서 파일을 쓰고 행을 바꾼다. 그래서 같은 끼니의 지우기·다른 올리기와
// 겹쳐도 주인 없는 파일이나 행이 남지 않는다(지우기가 먼저 끝나면 이쪽은 404, 이쪽이 먼저면 지우기가 새 파일까지 지운다).
func (s *Service) ReplaceMealPhotos(ctx context.Context, d Date, index int, photos [][]byte) ([]MealPhotoView, error) {
	base, err := s.photoDir()
	if err != nil {
		return nil, err
	}
	if err := checkMealIndex(index); err != nil {
		return nil, err
	}
	if len(photos) > MaxMealPhotos {
		return nil, badRequest("TOO_MANY_PHOTOS", fmt.Sprintf("사진은 %d장까지예요", MaxMealPhotos))
	}
	for _, b := range photos {
		if !isJPEG(b) {
			return nil, badRequest("INVALID_PHOTO", "JPEG 사진이어야 해요")
		}
		if len(b) > MaxPhotoBytes {
			return nil, photoTooLarge()
		}
	}
	day := string(d)
	var written []string // 이번에 새로 쓴 파일(실패하면 지운다)
	var old []string
	keep := map[string]bool{}
	err = s.tx(ctx, func(q *dbq.Queries) error {
		_, err := q.LockMealLog(ctx, dbq.LockMealLogParams{MealDate: d.Time(), MealIndex: int32(index)})
		if errors.Is(err, pgx.ErrNoRows) {
			return &Error{http.StatusNotFound, "MEAL_NOT_FOUND", "끼니 기록이 없어요"}
		}
		if err != nil {
			return err
		}
		if err := os.MkdirAll(filepath.Join(base, day), 0o750); err != nil {
			return err
		}
		// 잠금을 쥔 동안 이 끼니 파일을 쓰는 곳은 여기뿐이라, 남은 .partial은 전에 쓰다 멈춘 것이다
		removeStalePartials(filepath.Join(base, day), fmt.Sprintf("%s_%d_", day, index))
		rows := make([]dbq.InsertMealPhotoParams, 0, len(photos))
		for n, b := range photos {
			sum := sha256.Sum256(b)
			rel := filepath.Join(day, fmt.Sprintf("%s_%d_%d_%s.jpg", day, index, n, hex.EncodeToString(sum[:6])))
			keep[rel] = true
			path := filepath.Join(base, rel)
			if _, err := os.Stat(path); errors.Is(err, os.ErrNotExist) {
				if err := writeFileAtomic(path, b); err != nil {
					return err
				}
				written = append(written, rel)
			}
			rows = append(rows, dbq.InsertMealPhotoParams{MealDate: d.Time(), MealIndex: int32(index), N: int32(n), File: rel, Bytes: int32(len(b)), Sha256: sum[:]})
		}
		if old, err = q.DeleteMealPhotos(ctx, dbq.DeleteMealPhotosParams{MealDate: d.Time(), MealIndex: int32(index)}); err != nil {
			return err
		}
		for _, r := range rows {
			if err := q.InsertMealPhoto(ctx, r); err != nil {
				return err
			}
		}
		return nil
	})
	if err != nil {
		removeFiles(base, written)
		return nil, err
	}
	var gone []string
	for _, f := range old {
		if !keep[f] {
			gone = append(gone, f)
		}
	}
	removeFiles(base, gone)
	return s.MealPhotos(ctx, d, index)
}

// MealPhotos는 그 끼니에 저장된 사진 목록이다(사진 저장이 꺼져 있으면 빈 목록).
func (s *Service) MealPhotos(ctx context.Context, d Date, index int) ([]MealPhotoView, error) {
	if err := checkMealIndex(index); err != nil {
		return nil, err
	}
	ps, err := s.q().ListMealPhotos(ctx, dbq.ListMealPhotosParams{MealDate: d.Time(), MealIndex: int32(index)})
	if err != nil {
		return nil, err
	}
	out := make([]MealPhotoView, 0, len(ps))
	for _, p := range ps {
		out = append(out, photoView(p))
	}
	return out, nil
}

// MealPhotoPath는 사진 한 장의 파일 경로다. 없으면 404.
func (s *Service) MealPhotoPath(ctx context.Context, d Date, index, n int) (string, error) {
	base, err := s.photoDir()
	if err != nil {
		return "", err
	}
	if err := checkMealIndex(index); err != nil {
		return "", err
	}
	p, err := s.q().GetMealPhoto(ctx, dbq.GetMealPhotoParams{MealDate: d.Time(), MealIndex: int32(index), N: int32(n)})
	if errors.Is(err, pgx.ErrNoRows) {
		return "", &Error{http.StatusNotFound, "NOT_FOUND", "사진이 없어요"}
	}
	if err != nil {
		return "", err
	}
	return filepath.Join(base, p.File), nil
}

// deleteMealPhotoFiles는 끼니를 지운 뒤 남은 파일을 지운다(행은 같은 트랜잭션에서 이미 지웠다).
func (s *Service) deleteMealPhotoFiles(files []string) {
	if s.PhotoDir != "" {
		removeFiles(s.PhotoDir, files)
	}
}

func writeFileAtomic(path string, b []byte) error {
	tmp := path + partialSuffix
	if err := os.WriteFile(tmp, b, 0o640); err != nil {
		_ = os.Remove(tmp)
		return err
	}
	return os.Rename(tmp, path)
}

const partialSuffix = ".partial"

// removeStalePartials는 dir에서 prefix로 시작하는 쓰다 만 파일(*.partial)을 지운다(서버가 쓰는 중에 죽으면 남는다).
func removeStalePartials(dir, prefix string) {
	ents, err := os.ReadDir(dir)
	if err != nil {
		return
	}
	for _, e := range ents {
		if name := e.Name(); !e.IsDir() && strings.HasPrefix(name, prefix) && strings.HasSuffix(name, partialSuffix) {
			_ = os.Remove(filepath.Join(dir, name))
		}
	}
}

// removeFiles는 base 아래 상대 경로 파일을 지운다. 이미 없으면 넘어간다.
func removeFiles(base string, rels []string) {
	for _, rel := range rels {
		_ = os.Remove(filepath.Join(base, filepath.Clean(rel)))
	}
}
