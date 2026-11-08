// MyHome VM 하트비트 (Google Apps Script, 본인 Google 계정에서 돈다).
//
// 30분마다 밖(Google 서버)에서 두 주소를 부른다.
//   https://myhome.kodeploy.com/healthz         서버가 살아 있나 (VM·터널·웹 어디가 멈춰도 걸린다)
//   https://myhome.kodeploy.com/healthz/invest  자동매매가 조용히 멈추지 않았나 (금액은 안 실린다)
//     - 마지막 성공 status가 8일보다 오래됐거나, 마지막 judge·trade가 실패했으면 503
//     - 매매를 아직 시작하지 않았으면(position 없음) 200
// 각각 두 번 연속(약 1시간) 실패하면 스크립트 주인에게 메일을 한 통 보내고, 다시 살아나면 한 통 더 보낸다.
// VM이 죽었을 때(그래서 VM 안의 실패 알림도 못 보낼 때)와, 앱 알림을 놓친 매매 실패를 잡는 것이 목적이다.
//
// 설치: script.google.com → 새 프로젝트 → 이 파일 내용을 붙여 넣기 → install 실행(권한 허용: 외부 주소 호출, 메일 보내기, 트리거)
// 끄기: uninstall 실행
// 상태 보기: status 실행 → 실행 로그
// 두 주소 모두 Cloudflare Access의 /healthz 우회에 들어 있어야 한다 (docs/ACCESS_SETUP.md).

const TARGETS = [
  {
    key: 'web',
    url: 'https://myhome.kodeploy.com/healthz',
    downSubject: '[MyHome] 서버 응답 없음',
    upSubject: '[MyHome] 서버 다시 응답',
    hint:
      '자동매매 작업이 이 시간에 돌았다면 실패했을 수 있어요. VM이 멈추면 앱 알림도 오지 않아요.\n' +
      '확인: OCI 콘솔에서 kodeploy-home 인스턴스 상태, ssh home, journalctl -u myhome -u cloudflared\n',
  },
  {
    key: 'invest',
    url: 'https://myhome.kodeploy.com/healthz/invest',
    downSubject: '[MyHome] 자동매매 점검 필요',
    upSubject: '[MyHome] 자동매매 정상',
    hint:
      'status_stale: 주간 점검(status)이 8일 넘게 성공하지 않았어요. judge_failed·trade_failed: 마지막 판정·주문이 실패했어요.\n' +
      '확인: 앱 투자 탭, ssh home → journalctl -u myhome-invest-trade -u myhome-invest-judge -n 100, myhome invest status\n',
  },
];
const FAILS_BEFORE_MAIL = 2;

function probe(url) {
  try {
    const r = UrlFetchApp.fetch(url, { muteHttpExceptions: true, followRedirects: false });
    const ok = r.getResponseCode() === 200 && r.getContentText().indexOf('"ok"') >= 0;
    // 본문은 상태 낱말뿐이다(금액 없음). 실패 사유를 메일에 싣는다
    return { ok: ok, why: 'HTTP ' + r.getResponseCode() + ' ' + r.getContentText().slice(0, 200) };
  } catch (e) {
    return { ok: false, why: String(e) };
  }
}

function check() {
  const props = PropertiesService.getScriptProperties();
  const to = Session.getEffectiveUser().getEmail();
  const now = new Date();
  TARGETS.forEach(function (t) {
    const p = probe(t.url);
    // 예전 판(주소 하나)의 키 이름을 그대로 쓴다: web은 접두어 없음
    const k = function (name) { return t.key === 'web' ? name : t.key + '_' + name; };
    const fails = p.ok ? 0 : Number(props.getProperty(k('fails')) || 0) + 1;
    const down = props.getProperty(k('down')) === '1';
    const kv = {};
    kv[k('fails')] = String(fails);
    kv[k('last')] = now.toISOString() + ' ' + (p.ok ? 'ok' : p.why);
    props.setProperties(kv);

    if (!p.ok && fails >= FAILS_BEFORE_MAIL && !down) {
      const d = {};
      d[k('down')] = '1';
      d[k('downSince')] = now.toISOString();
      props.setProperties(d);
      MailApp.sendEmail(
        to,
        t.downSubject,
        t.url + ' 이 ' + fails + '번 연속(약 ' + fails * 30 + '분) 정상이 아니었어요.\n' + '마지막 결과: ' + p.why + '\n\n' + t.hint
      );
    }
    if (p.ok && down) {
      const since = props.getProperty(k('downSince')) || '';
      const u = {};
      u[k('down')] = '0';
      u[k('downSince')] = '';
      props.setProperties(u);
      MailApp.sendEmail(to, t.upSubject, t.url + ' 이 다시 정상이에요. 멈춘 시각(처음 확인): ' + since + '\n');
    }
  });
}

function install() {
  uninstall();
  ScriptApp.newTrigger('check').timeBased().everyMinutes(30).create();
  check();
  status();
}

function uninstall() {
  ScriptApp.getProjectTriggers().forEach(function (t) {
    ScriptApp.deleteTrigger(t);
  });
}

function status() {
  const p = PropertiesService.getScriptProperties().getProperties();
  Logger.log('트리거 %s개', ScriptApp.getProjectTriggers().length);
  Logger.log('서버: 마지막 %s, 연속 실패 %s, 다운 %s', p.last, p.fails, p.down);
  Logger.log('자동매매: 마지막 %s, 연속 실패 %s, 다운 %s', p.invest_last, p.invest_fails, p.invest_down);
}
