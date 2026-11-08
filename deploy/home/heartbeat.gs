// MyHome VM 하트비트 (Google Apps Script, 본인 Google 계정에서 돈다).
//
// 30분마다 밖(Google 서버)에서 https://myhome.kodeploy.com/healthz 를 부른다.
// 두 번 연속(약 1시간) 응답이 없으면 스크립트 주인에게 메일을 한 통 보내고, 다시 살아나면 한 통 더 보낸다.
// VM이 죽었을 때(그래서 VM 안의 실패 알림도 못 보낼 때)를 잡는 것이 목적이다. VM·터널·웹 어디가 멈춰도 걸린다.
//
// 설치: script.google.com → 새 프로젝트 → 이 파일 내용을 붙여 넣기 → install 실행(권한 허용: 외부 주소 호출, 메일 보내기, 트리거)
// 끄기: uninstall 실행
// 상태 보기: status 실행 → 실행 로그

const HEALTH_URL = 'https://myhome.kodeploy.com/healthz';
const FAILS_BEFORE_MAIL = 2;

function check() {
  const props = PropertiesService.getScriptProperties();
  let ok = false;
  let why = '';
  try {
    const r = UrlFetchApp.fetch(HEALTH_URL, { muteHttpExceptions: true, followRedirects: false });
    ok = r.getResponseCode() === 200 && r.getContentText().indexOf('"ok"') >= 0;
    why = 'HTTP ' + r.getResponseCode();
  } catch (e) {
    why = String(e);
  }
  const now = new Date();
  const fails = ok ? 0 : Number(props.getProperty('fails') || 0) + 1;
  const down = props.getProperty('down') === '1';
  props.setProperties({ fails: String(fails), last: now.toISOString() + ' ' + (ok ? 'ok' : why) });

  const to = Session.getEffectiveUser().getEmail();
  if (!ok && fails >= FAILS_BEFORE_MAIL && !down) {
    props.setProperties({ down: '1', downSince: now.toISOString() });
    MailApp.sendEmail(
      to,
      '[MyHome] 서버 응답 없음',
      'myhome.kodeploy.com 이 ' + fails + '번 연속(약 ' + fails * 30 + '분) 응답하지 않았어요.\n' +
        '마지막 결과: ' + why + '\n\n' +
        '자동매매 작업이 이 시간에 돌았다면 실패했을 수 있어요. VM이 멈추면 앱 알림도 오지 않아요.\n' +
        '확인: OCI 콘솔에서 kodeploy-home 인스턴스 상태, ssh home, journalctl -u myhome -u cloudflared\n'
    );
  }
  if (ok && down) {
    const since = props.getProperty('downSince') || '';
    props.setProperties({ down: '0', downSince: '' });
    MailApp.sendEmail(to, '[MyHome] 서버 다시 응답', 'myhome.kodeploy.com 이 다시 응답해요. 멈춘 시각(처음 확인): ' + since + '\n');
  }
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
  Logger.log('트리거 %s개, 마지막 확인 %s, 연속 실패 %s, 다운 %s', ScriptApp.getProjectTriggers().length, p.last, p.fails, p.down);
}
