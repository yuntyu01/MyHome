package health

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"os"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/yuntyu01/MyHome/server/internal/ai"
	"github.com/yuntyu01/MyHome/server/internal/health/foods"
	"github.com/yuntyu01/MyHome/server/internal/httpx"
)

// 사진 분석 요청 크기. 앱은 사진을 긴 변 1024px·JPEG 85로 줄여서 보낸다(보통 0.3MB). 전체 서버 본문 제한(MAX_BODY_BYTES)보다
// 크므로 서버 조립에서 이 경로만 MaxAnalyzeBytes로 연다(httpx.LimitBodyRoutes).
// 한 장 2MB: VM 메모리(MemoryMax=200M)에서 분석 요청 하나가 사진을 base64·JSON으로 몇 번 복사해도 넉넉하게(예전 5MB·16MB).
const (
	AnalyzePath = "/v1/health/meals/analyze"
	// MealPhotosPath도 사진을 받으므로 서버 조립에서 MaxAnalyzeBytes로 연다
	MealPhotosPath  = "/v1/health/meals/photos"
	MaxPhotoBytes   = 2 << 20
	MaxAnalyzeBytes = ai.MaxPhotos*MaxPhotoBytes + 1<<20 // 7MB
	// AnalyzeBudget은 분석 요청 하나에 쓰는 시간이다(차례 기다리기 + Gemini 두 번까지). 서버 WriteTimeout(60초)보다 짧게.
	AnalyzeBudget = 55 * time.Second
)

// Handler는 /v1/health/* JSON API다. JSON 필드와 쿼리 이름은 핏두잉 서버와 같다(camelCase).
type Handler struct {
	Svc      *Service
	Analyzer ai.FoodAnalyzer // 사진 분석(mock 또는 gemini)
	Foods    *foods.Lookup
	Logger   *slog.Logger

	// 사진을 메모리에 올리는 요청은 종류마다 한 번에 하나씩 한다(분석 하나, 사진 보관 하나) - 메모리 상한 때문
	slotsOnce              sync.Once
	analyzeSlot, photoSlot chan struct{}
}

// acquire는 slot 차례를 기다린다. 기다리다 요청이 끝나면(취소·시간 초과) 503 BUSY.
func (h *Handler) acquire(ctx context.Context, slot func(*Handler) chan struct{}) (release func(), err error) {
	h.slotsOnce.Do(func() { h.analyzeSlot, h.photoSlot = make(chan struct{}, 1), make(chan struct{}, 1) })
	c := slot(h)
	select {
	case c <- struct{}{}:
		return func() { <-c }, nil
	case <-ctx.Done():
		return nil, &Error{http.StatusServiceUnavailable, "BUSY", "다른 사진을 처리하는 중이에요. 잠시 뒤 다시 해 주세요"}
	}
}

type handlerFunc func(w http.ResponseWriter, r *http.Request) error

// Register는 라우트를 등록한다. wrap은 인증 미들웨어다(기기 토큰 또는 웹 세션).
func (h *Handler) Register(mux *http.ServeMux, wrap func(http.Handler) http.Handler) {
	route := func(pattern string, fn handlerFunc) { mux.Handle(pattern, wrap(h.handle(fn))) }
	route("GET /v1/health/today", h.today)
	route("GET /v1/health/records/month", h.month)
	route("GET /v1/health/records/day", h.day)
	route("GET /v1/health/weekly", h.weekly)
	route("POST "+AnalyzePath, h.analyze)
	route("POST /v1/health/meals", h.postMeal)
	route("GET /v1/health/meals", h.getMeals)
	route("DELETE /v1/health/meals", h.deleteMeal)
	route("PUT "+MealPhotosPath, h.putMealPhotos)
	route("GET "+MealPhotosPath, h.getMealPhotos)
	route("GET /v1/health/meals/photos/{date}/{index}/{n}", h.getMealPhoto)
	route("POST /v1/health/gym/check-in", h.checkIn)
	route("GET /v1/health/rest", h.getRest)
	route("POST /v1/health/rest", h.postRest)
	route("GET /v1/health/settings", h.getSettings)
	route("PUT /v1/health/settings", h.putSettings)
	route("GET /v1/health/targets/recommend", h.recommend)
	route("GET /v1/health/foods/barcode/{code}", h.barcode)
	route("GET /v1/health/body", h.getBody)
	route("PUT /v1/health/body", h.putBody)
	route("DELETE /v1/health/body", h.deleteBody)
	route("POST "+BodyScanPath, h.scanBody)
}

// handle은 에러를 {"error": 메시지, "code": 코드}로 쓴다. 모르는 에러는 로그를 남기고 500.
func (h *Handler) handle(fn handlerFunc) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		err := fn(w, r)
		if err == nil {
			return
		}
		e := asError(err)
		if e == nil {
			h.Logger.Error("건강 API 오류", "path", r.URL.Path, "err", err)
			e = &Error{http.StatusInternalServerError, "INTERNAL", "internal error"}
		}
		httpx.JSON(w, e.Status, map[string]string{"error": e.Message, "code": e.Code})
	})
}

// decode는 JSON 본문을 읽는다. 모르는 필드는 무시한다(핏두잉 모양 그대로 보내도 되게).
func decode(r *http.Request, v any) error {
	if err := json.NewDecoder(r.Body).Decode(v); err != nil {
		var tooBig *http.MaxBytesError
		if errors.As(err, &tooBig) {
			return &Error{http.StatusRequestEntityTooLarge, "TOO_LARGE", "요청이 너무 커요"}
		}
		return badRequest("INVALID", "요청 형식이 잘못됐어요")
	}
	return nil
}

// queryDate는 필수 YYYY-MM-DD 쿼리 값이다.
func queryDate(r *http.Request, name string) (Date, error) {
	s := r.URL.Query().Get(name)
	if s == "" {
		return "", invalid(name, "필수예요")
	}
	d, err := ParseDate(s)
	if err != nil {
		return "", invalid(name, "YYYY-MM-DD 형식이어야 해요")
	}
	return d, nil
}

// ── 화면 ──────────────────────────────────────────────────────────────

// today: GET /v1/health/today?date=YYYY-MM-DD&time=HH:mm. 기기 시각은 time(핏두잉 이름) 또는 now로 받고 없어도 된다.
func (h *Handler) today(w http.ResponseWriter, r *http.Request) error {
	date, err := queryDate(r, "date")
	if err != nil {
		return err
	}
	field, now := "time", r.URL.Query().Get("time")
	if now == "" {
		field, now = "now", r.URL.Query().Get("now")
	}
	if now != "" && !ValidHHMM(now) {
		return invalid(field, "HH:mm 형식이어야 해요")
	}
	out, err := h.Svc.Today(r.Context(), date, now)
	if err != nil {
		return err
	}
	httpx.JSON(w, http.StatusOK, out)
	return nil
}

func (h *Handler) month(w http.ResponseWriter, r *http.Request) error {
	out, err := h.Svc.Month(r.Context(), r.URL.Query().Get("month"))
	if err != nil {
		return err
	}
	httpx.JSON(w, http.StatusOK, out)
	return nil
}

func (h *Handler) day(w http.ResponseWriter, r *http.Request) error {
	date, err := queryDate(r, "date")
	if err != nil {
		return err
	}
	out, err := h.Svc.Day(r.Context(), date)
	if err != nil {
		return err
	}
	httpx.JSON(w, http.StatusOK, out)
	return nil
}

func (h *Handler) weekly(w http.ResponseWriter, r *http.Request) error {
	date, err := queryDate(r, "date")
	if err != nil {
		return err
	}
	out, err := h.Svc.Weekly(r.Context(), date)
	if err != nil {
		return err
	}
	httpx.JSON(w, http.StatusOK, out)
	return nil
}

// ── 끼니 ──────────────────────────────────────────────────────────────

// analyze: 한 끼니 사진 1~3장을 "photo" 필드로 받아 한 번에 분석한다. 사진은 저장하지 않는다(메모리에서 읽고 버림).
// 응답 헤더 X-AI-Model은 쓴 모델이다. 한 번에 하나만 분석하고(차례를 기다린다), AI 크레딧이 없으면 503 AI_NO_CREDIT.
func (h *Handler) analyze(w http.ResponseWriter, r *http.Request) error {
	ctx, cancel := context.WithTimeout(r.Context(), AnalyzeBudget)
	defer cancel()
	// 본문을 읽기 전에 차례를 기다린다: 기다리는 요청은 사진을 메모리에 올리지 않는다
	release, err := h.acquire(ctx, func(h *Handler) chan struct{} { return h.analyzeSlot })
	if err != nil {
		return err
	}
	defer release()
	photos, err := readPhotos(w, r)
	if err != nil {
		return err
	}
	if len(photos) == 0 {
		return badRequest("INVALID_PHOTO", "사진 파일이 필요해요")
	}
	res, err := h.Analyzer.Analyze(ctx, photos)
	if err != nil {
		h.Logger.Warn("음식 인식 실패", "err", err)
		if ai.NoCredit(err) {
			return &Error{http.StatusServiceUnavailable, "AI_NO_CREDIT", "AI 크레딧이 없어요 - 직접 입력해 주세요"}
		}
		return &Error{http.StatusBadGateway, "AI_FAILED", "음식 인식에 실패했어요. 다시 알아보거나 직접 입력해 주세요"}
	}
	if res.Items == nil {
		res.Items = []ai.Item{}
	}
	if m := ai.ModelName(h.Analyzer); m != "" {
		w.Header().Set("X-AI-Model", m)
	}
	httpx.JSON(w, http.StatusOK, res)
	return nil
}

// readPhotos는 multipart 본문의 "photo" 파트를 차례로 읽는다(ai.MaxPhotos장까지, 한 장 MaxPhotoBytes까지, image/*).
// ParseMultipartForm처럼 본문 전체를 한 번 더 버퍼에 담지 않고 파트마다 한 번만 복사한다. 다른 이름의 파트는 버린다.
func readPhotos(w http.ResponseWriter, r *http.Request) ([]ai.Photo, error) {
	if r.ContentLength > MaxAnalyzeBytes {
		return nil, photoTooLarge()
	}
	r.Body = http.MaxBytesReader(w, r.Body, MaxAnalyzeBytes)
	mr, err := r.MultipartReader()
	if err != nil {
		return nil, badRequest("INVALID_PHOTO", "사진 파일이 필요해요")
	}
	var photos []ai.Photo
	for {
		p, err := mr.NextPart()
		if errors.Is(err, io.EOF) {
			return photos, nil
		}
		if err != nil {
			return nil, multipartError(err)
		}
		if p.FormName() != "photo" {
			if _, err := io.Copy(io.Discard, p); err != nil {
				return nil, multipartError(err)
			}
			continue
		}
		if len(photos) == ai.MaxPhotos {
			return nil, badRequest("TOO_MANY_PHOTOS", fmt.Sprintf("사진은 한 번에 %d장까지예요", ai.MaxPhotos))
		}
		data, err := io.ReadAll(io.LimitReader(p, MaxPhotoBytes+1))
		if err != nil {
			return nil, multipartError(err)
		}
		contentType := p.Header.Get("Content-Type")
		switch {
		case len(data) > MaxPhotoBytes:
			return nil, photoTooLarge()
		case len(data) == 0 || !strings.HasPrefix(contentType, "image/"):
			return nil, badRequest("INVALID_PHOTO", "사진 파일이 필요해요")
		}
		photos = append(photos, ai.Photo{Data: data, ContentType: contentType})
	}
}

// multipartError는 본문 읽기 실패다: 전체 제한을 넘었으면 413, 아니면 형식 오류.
func multipartError(err error) error {
	var tooBig *http.MaxBytesError
	if errors.As(err, &tooBig) {
		return photoTooLarge()
	}
	return badRequest("INVALID_PHOTO", "multipart 사진이 필요해요")
}

func photoTooLarge() error {
	return &Error{http.StatusRequestEntityTooLarge, "PHOTO_TOO_LARGE", "사진이 너무 커요"}
}

func (h *Handler) postMeal(w http.ResponseWriter, r *http.Request) error {
	var req MealRequest
	if err := decode(r, &req); err != nil {
		return err
	}
	v, err := h.Svc.LogMeal(r.Context(), req)
	if err != nil {
		return err
	}
	httpx.JSON(w, http.StatusOK, map[string]any{"meal": v})
	return nil
}

func (h *Handler) getMeals(w http.ResponseWriter, r *http.Request) error {
	date, err := queryDate(r, "date")
	if err != nil {
		return err
	}
	out, err := h.Svc.MealsOn(r.Context(), date)
	if err != nil {
		return err
	}
	httpx.JSON(w, http.StatusOK, out)
	return nil
}

func (h *Handler) deleteMeal(w http.ResponseWriter, r *http.Request) error {
	date, err := queryDate(r, "date")
	if err != nil {
		return err
	}
	idx, err := strconv.Atoi(r.URL.Query().Get("index"))
	if err != nil {
		return invalid("index", "숫자여야 해요")
	}
	if err := h.Svc.DeleteMeal(r.Context(), date, idx); err != nil {
		return err
	}
	w.WriteHeader(http.StatusNoContent)
	return nil
}

// putMealPhotos: PUT /v1/health/meals/photos?date=&index= , multipart "photo" 0~3장(JPEG). 그 끼니 사진을 통째로 바꾼다.
// 끼니 기록이 없으면 404 MEAL_NOT_FOUND(앱은 올리기를 그만둔다).
func (h *Handler) putMealPhotos(w http.ResponseWriter, r *http.Request) error {
	date, idx, err := mealKey(r)
	if err != nil {
		return err
	}
	release, err := h.acquire(r.Context(), func(h *Handler) chan struct{} { return h.photoSlot })
	if err != nil {
		return err
	}
	defer release()
	ps, err := readPhotos(w, r)
	if err != nil {
		return err
	}
	photos := make([][]byte, 0, len(ps))
	for _, p := range ps {
		photos = append(photos, p.Data)
	}
	out, err := h.Svc.ReplaceMealPhotos(r.Context(), date, idx, photos)
	if err != nil {
		return err
	}
	httpx.JSON(w, http.StatusOK, map[string]any{"photos": out})
	return nil
}

func (h *Handler) getMealPhotos(w http.ResponseWriter, r *http.Request) error {
	date, idx, err := mealKey(r)
	if err != nil {
		return err
	}
	out, err := h.Svc.MealPhotos(r.Context(), date, idx)
	if err != nil {
		return err
	}
	httpx.JSON(w, http.StatusOK, map[string]any{"photos": out})
	return nil
}

func (h *Handler) getMealPhoto(w http.ResponseWriter, r *http.Request) error {
	date, err := ParseDate(r.PathValue("date"))
	if err != nil {
		return invalid("date", "YYYY-MM-DD 형식이어야 해요")
	}
	idx, err1 := strconv.Atoi(r.PathValue("index"))
	n, err2 := strconv.Atoi(r.PathValue("n"))
	if err1 != nil || err2 != nil {
		return invalid("index", "숫자여야 해요")
	}
	path, err := h.Svc.MealPhotoPath(r.Context(), date, idx, n)
	if err != nil {
		return err
	}
	f, err := os.Open(path)
	if errors.Is(err, os.ErrNotExist) {
		return &Error{http.StatusNotFound, "NOT_FOUND", "사진 파일이 없어요"}
	}
	if err != nil {
		return err
	}
	defer f.Close()
	w.Header().Set("Content-Type", "image/jpeg")
	w.Header().Set("Cache-Control", "private, max-age=86400")
	_, err = io.Copy(w, f)
	return err
}

// mealKey는 ?date=&index= 를 읽는다.
func mealKey(r *http.Request) (Date, int, error) {
	date, err := queryDate(r, "date")
	if err != nil {
		return "", 0, err
	}
	idx, err := strconv.Atoi(r.URL.Query().Get("index"))
	if err != nil {
		return "", 0, invalid("index", "숫자여야 해요")
	}
	return date, idx, nil
}

// ── 헬스·휴식권 ────────────────────────────────────────────────────────

func (h *Handler) checkIn(w http.ResponseWriter, r *http.Request) error {
	var b struct {
		Date       Date       `json:"date"`
		VerifiedBy Verified   `json:"verifiedBy"`
		CheckInAt  *time.Time `json:"checkInAt"`
	}
	if err := decode(r, &b); err != nil {
		return err
	}
	switch {
	case b.Date.IsZero():
		return invalid("date", "필수예요")
	case b.VerifiedBy == "":
		return invalid("verifiedBy", "필수예요")
	case b.VerifiedBy == Rest:
		return badRequest("INVALID", "휴식은 /v1/health/rest 로 처리해요")
	case b.VerifiedBy != Photo && b.VerifiedBy != Geofence:
		return invalid("verifiedBy", "photo 또는 geofence여야 해요")
	}
	// geofence: 앱이 헬스장 반경 안에 몇 분 머문 것을 폰에서 확인하고 보낸다(좌표는 폰에만 있다, 2026-11-08 다시 켬)
	out, err := h.Svc.GymCheckIn(r.Context(), b.Date, b.VerifiedBy, b.CheckInAt)
	if err != nil {
		return err
	}
	httpx.JSON(w, http.StatusOK, out)
	return nil
}

// restDate는 휴식권 날짜다: ?date=, 없으면 JSON 본문 {"date"}(POST), 그것도 없으면 오늘(KST).
func (h *Handler) restDate(r *http.Request) (Date, error) {
	if r.URL.Query().Has("date") {
		return queryDate(r, "date")
	}
	if r.Method == http.MethodPost {
		var b struct {
			Date Date `json:"date"`
		}
		err := json.NewDecoder(r.Body).Decode(&b)
		switch {
		case errors.Is(err, io.EOF): // 본문 없음
		case err != nil:
			return "", badRequest("INVALID", "요청 형식이 잘못됐어요")
		case !b.Date.IsZero():
			return b.Date, nil
		}
	}
	return h.Svc.today(), nil
}

func (h *Handler) getRest(w http.ResponseWriter, r *http.Request) error {
	date, err := h.restDate(r)
	if err != nil {
		return err
	}
	out, err := h.Svc.Rest(r.Context(), date)
	if err != nil {
		return err
	}
	httpx.JSON(w, http.StatusOK, out)
	return nil
}

// postRest는 그날 헬스를 휴식권으로 쉰다(핏두잉 POST /api/coupons/rest).
func (h *Handler) postRest(w http.ResponseWriter, r *http.Request) error {
	date, err := h.restDate(r)
	if err != nil {
		return err
	}
	out, err := h.Svc.UseRest(r.Context(), date)
	if err != nil {
		return err
	}
	httpx.JSON(w, http.StatusOK, out)
	return nil
}

// ── 바코드 ────────────────────────────────────────────────────────────

var barcodeRe = regexp.MustCompile(`^\d{8,14}$`)

// barcode: GET /v1/health/foods/barcode/{code}. 제품 + 영양성분(공공 식품 데이터). 키가 없으면 503.
func (h *Handler) barcode(w http.ResponseWriter, r *http.Request) error {
	code := r.PathValue("code")
	if !barcodeRe.MatchString(code) {
		return invalid("code", "바코드는 숫자 8~14자리예요")
	}
	if !h.Foods.Available() {
		return &Error{http.StatusServiceUnavailable, "BARCODE_LOOKUP_UNAVAILABLE",
			"바코드로 제품을 찾을 수 없어요. 영양성분표를 찍거나 직접 입력해 주세요"}
	}
	p, err := h.Foods.Search(r.Context(), code)
	switch {
	case errors.Is(err, foods.ErrNotFound):
		return &Error{http.StatusNotFound, "BARCODE_NOT_FOUND", "등록된 제품이 아니에요. 영양성분표를 찍거나 직접 입력해 주세요"}
	case err != nil && p.Name == "":
		h.Logger.Warn("바코드 조회 실패", "err", err)
		return &Error{http.StatusBadGateway, "BARCODE_LOOKUP_FAILED", "제품을 찾지 못했어요. 잠시 뒤 다시 해 주세요"}
	case err != nil:
		// 제품은 알고 영양성분만 실패: 이름만 준다
		h.Logger.Warn("영양성분 조회 실패", "err", err)
	}
	httpx.JSON(w, http.StatusOK, foods.Answer{Product: p, Result: foods.ResultOf(p)})
	return nil
}

// ── 설정·목표 ──────────────────────────────────────────────────────────

// SettingsView는 설정 응답이다(핏두잉처럼 {settings: ...}로 감싼다. 강도 하향 대기 값은 뺐다).
type SettingsView struct {
	Settings Settings `json:"settings"`
}

func (h *Handler) getSettings(w http.ResponseWriter, r *http.Request) error {
	st, err := h.Svc.Settings(r.Context())
	if err != nil {
		return err
	}
	httpx.JSON(w, http.StatusOK, SettingsView{st})
	return nil
}

// putSettings는 보낸 키만 바꾼다(Service.PutSettings).
func (h *Handler) putSettings(w http.ResponseWriter, r *http.Request) error {
	body, err := io.ReadAll(r.Body)
	if err != nil {
		var tooBig *http.MaxBytesError
		if errors.As(err, &tooBig) {
			return &Error{http.StatusRequestEntityTooLarge, "TOO_LARGE", "요청이 너무 커요"}
		}
		return badRequest("INVALID", "요청 형식이 잘못됐어요")
	}
	st, err := h.Svc.PutSettings(r.Context(), body)
	if err != nil {
		return err
	}
	httpx.JSON(w, http.StatusOK, SettingsView{st})
	return nil
}

// recommend는 목표를 추천한다. 설정은 바꾸지 않는다(결과를 적용할지는 사용자가 정한다, 1200kcal 이상).
//   - goal, weightKg만: 체중 기반 옛 값.
//   - heightCm, age, sex, activity까지: 2025 KDRI 성인 EER(mode=balanced|training).
func (h *Handler) recommend(w http.ResponseWriter, r *http.Request) error {
	q := r.URL.Query()
	goal, err := ParseGoal(q.Get("goal"))
	if err != nil {
		return err
	}
	kg, err := strconv.ParseFloat(q.Get("weightKg"), 64)
	if err != nil || kg <= 0 {
		return invalid("weightKg", "숫자여야 해요")
	}
	body := []string{"heightCm", "age", "sex", "activity"}
	given := 0
	for _, k := range body {
		if q.Has(k) {
			given++
		}
	}
	if given == 0 {
		if goal == GoalBulk {
			return invalid("goal", "bulk 는 heightCm·age·sex·activity 와 함께 요청해야 해요")
		}
		httpx.JSON(w, http.StatusOK, Recommend(goal, kg))
		return nil
	}
	if given < len(body) {
		return invalid("body", "heightCm, age, sex, activity 를 모두 보내야 해요")
	}
	cm, err := strconv.ParseFloat(q.Get("heightCm"), 64)
	if err != nil || cm < 100 || cm > 250 {
		return invalid("heightCm", "100~250 사이 숫자여야 해요")
	}
	age, err := strconv.Atoi(q.Get("age"))
	if err != nil || !between(age, AgeMin, AgeMax) {
		return invalid("age", "10~100 사이 정수여야 해요")
	}
	var sex Sex
	if err := sex.UnmarshalJSON([]byte(strconv.Quote(q.Get("sex")))); err != nil || sex == "" {
		return invalid("sex", "male, female 중 하나예요")
	}
	act, ok := ParseActivity(q.Get("activity"))
	if !ok {
		return invalid("activity", "sedentary, low, active, veryActive 중 하나예요")
	}
	rec, err := RecommendEER(goal, Body{Sex: sex, Age: age, Activity: act, HeightCm: cm, WeightKg: kg}, q.Get("mode"))
	if err != nil {
		return err
	}
	httpx.JSON(w, http.StatusOK, rec)
	return nil
}
