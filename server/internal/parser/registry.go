package parser

import (
	"fmt"
	"strings"
)

// Registry는 패키지명으로 파서를 고른다.
type Registry struct {
	byPackage map[string]Parser
	detectors []Parser
	fallback  Parser
}

// Builtin은 내장 파서 목록이다. 새 소스를 추가하려면 Parser를 구현하고 여기에 넣는다.
func Builtin() []Parser { return []Parser{KB{}, Toss{}} }

// NewRegistry는 매핑 설정으로 레지스트리를 만든다.
//
// mapping 형식(환경변수 PARSER_PACKAGES): "이름=패키지1,패키지2;이름2=패키지3"
// 이름이 내장 파서(kb, toss)면 그 파서를, 아니면 범용 파서를 그 이름(source)으로 쓴다.
// 패키지명은 코드에 박지 않는다(앱 업데이트로 바뀔 수 있고 확인되지 않은 사실이라서).
func NewRegistry(mapping string) (*Registry, error) {
	r := &Registry{byPackage: map[string]Parser{}, detectors: Builtin(), fallback: Generic{}}
	byName := map[string]Parser{}
	for _, p := range r.detectors {
		byName[p.Name()] = p
	}
	for _, group := range strings.Split(mapping, ";") {
		group = strings.TrimSpace(group)
		if group == "" {
			continue
		}
		name, pkgs, ok := strings.Cut(group, "=")
		name = strings.TrimSpace(name)
		if !ok || name == "" {
			return nil, fmt.Errorf("PARSER_PACKAGES 형식 오류: %q (이름=패키지,패키지)", group)
		}
		p, ok := byName[name]
		if !ok {
			p = Generic{Source: name}
		}
		for _, pkg := range strings.Split(pkgs, ",") {
			if pkg = strings.TrimSpace(pkg); pkg != "" {
				r.byPackage[pkg] = p
			}
		}
	}
	return r, nil
}

// For는 이벤트에 맞는 파서를 고른다: 패키지 매핑 → 내용 감지 → 범용.
func (r *Registry) For(ev Event) Parser {
	if p, ok := r.byPackage[ev.Package]; ok {
		return p
	}
	for _, p := range r.detectors {
		if p.Detect(ev) {
			return p
		}
	}
	return r.fallback
}

// Parse는 알맞은 파서로 해석한다.
func (r *Registry) Parse(ev Event) (Result, error) { return r.For(ev).Parse(ev) }
