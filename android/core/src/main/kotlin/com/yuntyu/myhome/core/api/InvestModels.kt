package com.yuntyu.myhome.core.api

import kotlinx.serialization.SerialName
import kotlinx.serialization.Serializable

/**
 * GET /v1/invest 응답(앱 투자 탭 첫 화면). 웹 투자 첫 화면과 같은 값이고, 시장 숫자(평가액·가격·환율)는 없다.
 * 문구(제목·설명·값)는 서버가 만들어 보낸다. 앱은 그대로 그리고, bad면 경고색만 입힌다.
 */
@Serializable
data class InvestStatus(
    val migrated: Boolean = false,
    val symbol: String = "",
    val shares: Int = 0,
    @SerialName("price_stale") val priceStale: Boolean = false,
    @SerialName("last_price_day") val lastPriceDay: String = "",
    @SerialName("last_price_label") val lastPriceLabel: String = "",
    val month: InvestRow? = null,
    val schedule: List<InvestRow>? = null,
    val checks: List<InvestRow>? = null,
    val problems: Int = 0,
    /** 맨 위 요약: 종목·증권 총액·넣은 돈(세전, 토스 앱과 같은 기준) */
    val summary: InvestSummary? = null,
    /** 이번 달 자동 매수 단계(판정 검산 → 돈 입금 → 환전 → 구매 신청 → 구매 성공) */
    val flow: InvestFlow? = null,
    /** 입금일(한국 날짜 1~20). 0이면 정하지 않음 = 첫 거래일에 산다 */
    @SerialName("deposit_day") val depositDay: Int = 0,
    /** 매일 기록: 오늘 신호(참고, 판정은 달 마지막 거래일)와 거래일마다의 원금·금액·수익률 */
    val daily: InvestDaily? = null,
)

@Serializable
data class InvestDaily(
    val has: Boolean = false,
    /** 마지막 기록일 종가로 본 신호 종목(TQQQ·QQQM) */
    val signal: String = "",
    /** 이번 달 판정 종목과 같은가 */
    val same: Boolean = true,
    val title: String = "",
    val lines: List<String> = emptyList(),
    /** 최근 날부터 */
    val rows: List<InvestDailyRow> = emptyList(),
)

@Serializable
data class InvestDailyRow(
    val day: String = "",
    val signal: String = "",
    /** S&P500이 185일선보다 "+8.5%" */
    val trend: String = "",
    /** 나스닥100 21일 변동성 "17.9%" */
    val vol: String = "",
    val c1: Boolean = false,
    val c2: Boolean = false,
    @SerialName("has_money") val hasMoney: Boolean = false,
    @SerialName("invested_krw") val investedKrw: Long = 0,
    @SerialName("total_krw") val totalKrw: Long = 0,
    @SerialName("gain_krw") val gainKrw: Long = 0,
    @SerialName("gain_pct") val gainPct: String = "",
)

@Serializable
data class InvestSummary(
    val symbol: String = "",
    val shares: Int = 0,
    /** 이번 적용월 신호 종목 */
    val target: String = "",
    @SerialName("can_value") val canValue: Boolean = false,
    @SerialName("total_krw") val totalKrw: Long = 0,
    @SerialName("stock_krw") val stockKrw: Long = 0,
    @SerialName("cash_krw") val cashKrw: Long = 0,
    @SerialName("invested_krw") val investedKrw: Long = 0,
    @SerialName("gain_krw") val gainKrw: Long = 0,
    @SerialName("gain_pct") val gainPct: String = "",
    val basis: String = "",
)

/** state: done(끝남) · running(진행 중) · waiting(아직) · failed(문제) · skipped(해당 없음) */
@Serializable
data class InvestFlow(
    val ym: String = "",
    val title: String = "",
    val state: String = "",
    val value: String = "",
    val steps: List<InvestStep> = emptyList(),
)

@Serializable
data class InvestStep(
    val key: String = "",
    val title: String = "",
    val state: String = "",
    val value: String = "",
    /** 눌렀을 때 펼치는 근거 줄(서버가 만든 문구) */
    val lines: List<String> = emptyList(),
)

@Serializable
data class InvestRow(
    val title: String = "",
    val meta: String = "",
    val value: String = "",
    val bad: Boolean = false,
)

/** GET /v1/invest/value 응답(평가 화면). 금액은 원 단위 정수, 현금은 서버가 "$12.50"처럼 만든 문자열. */
@Serializable
data class InvestValue(
    val migrated: Boolean = false,
    val symbol: String = "",
    val shares: Int = 0,
    @SerialName("price_stale") val priceStale: Boolean = false,
    @SerialName("last_price_label") val lastPriceLabel: String = "",
    @SerialName("can_value") val canValue: Boolean = false,
    val why: String = "",
    @SerialName("net_krw") val netKrw: Long = 0,
    val basis: String = "",
    @SerialName("contributed_krw") val contributedKrw: Long = 0,
    @SerialName("gain_krw") val gainKrw: Long = 0,
    @SerialName("gain_pct") val gainPct: String = "",
    @SerialName("tax_krw") val taxKrw: Long = 0,
    @SerialName("cash_usd") val cashUsd: String = "",
    /** 토스가 마지막으로 알려 준 주문가능 현금("$300.51")과 그 시각("11/16 11:23"). 아직 없으면 빈 값 */
    @SerialName("broker_cash_usd") val brokerCashUsd: String = "",
    @SerialName("broker_cash_at") val brokerCashAt: String = "",
    /** 토스 − 장부("+$14.50"). 1달러 미만이면 빈 값 */
    @SerialName("cash_gap_usd") val cashGapUsd: String = "",
    val ledger: List<InvestLedgerRow>? = null,
    @SerialName("ledger_sum") val ledgerSum: Long = 0,
    @SerialName("ledger_gap") val ledgerGap: Long = 0,
    @SerialName("ledger_bad") val ledgerBad: Boolean = false,
)

@Serializable
data class InvestLedgerRow(
    val on: String = "",
    val ym: String = "",
    val source: String = "",
    val shares: String = "",
    val note: String = "",
    val amount: Long = 0,
)

/** GET /v1/invest/alerts 응답. 자동매매 작업(systemd)이 실패할 때와 그 달 매수가 끝났을 때(한 번) 한 줄씩 생긴다. */
@Serializable
data class InvestAlerts(val alerts: List<InvestAlert> = emptyList())

@Serializable
data class InvestAlert(
    val id: Long,
    @SerialName("created_at") val createdAt: String = "",
    /** fail(실패, 소리 있는 채널) 또는 done(매수 완료, 조용한 채널). 칸이 없는 옛 서버는 다 실패다 */
    val kind: String = "fail",
    val title: String = "",
    val body: String = "",
)

/**
 * GET /v1/invest/backtest — 이 전략의 고정 백테스트 표(TQ 뷰어 「백테스트 결과」 이식). 서버가 다시 계산하지 않는다.
 * 금액은 억원, 비율은 %. tops는 그 행이 항목별 1등인 열 키(손실%는 낮을수록, MDD는 0에 가까울수록 1등).
 */
@Serializable
data class Backtest(
    val period: String = "",
    @SerialName("fee_rate") val feeRate: Double = 0.0,
    val unit: String = "",
    val columns: List<BacktestColumn> = emptyList(),
    /** 7년 적립·전체 표본에서 ★와 TQQQ·QLD·QQQ 단순 보유 */
    val key: List<BacktestRow> = emptyList(),
    val horizons: List<BacktestHorizon> = emptyList(),
    val notes: List<String> = emptyList(),
)

@Serializable
data class BacktestColumn(
    val key: String,
    val label: String,
    val percent: Boolean = false,
    @SerialName("higher_is_better") val higherIsBetter: Boolean = true,
)

@Serializable
data class BacktestHorizon(
    val label: String,
    @SerialName("ex_dotcom") val exDotcom: List<BacktestRow> = emptyList(),
    val all: List<BacktestRow> = emptyList(),
)

@Serializable
data class BacktestRow(
    val name: String,
    val star: Boolean = false,
    val principal: Double = 0.0,
    val worst: Double = 0.0,
    val q10: Double = 0.0,
    val q30: Double = 0.0,
    val median: Double = 0.0,
    val q70: Double = 0.0,
    val q90: Double = 0.0,
    val mean: Double = 0.0,
    val best: Double = 0.0,
    @SerialName("loss_pct") val lossPct: Double = 0.0,
    val windows: Int = 0,
    @SerialName("mdd_median_pct") val mddMedianPct: Double = 0.0,
    @SerialName("mdd_worst_pct") val mddWorstPct: Double = 0.0,
    val tops: List<String> = emptyList(),
) {
    fun value(key: String): Double = when (key) {
        "worst" -> worst
        "q10" -> q10
        "q30" -> q30
        "median" -> median
        "q70" -> q70
        "q90" -> q90
        "loss_pct" -> lossPct
        "mdd_median_pct" -> mddMedianPct
        "mdd_worst_pct" -> mddWorstPct
        else -> Double.NaN
    }

    /** 칸 글자. 금액은 억 소수 둘째 자리, 손실%는 소수 첫째, MDD는 정수 %. 음수는 진짜 빼기표(−) */
    fun text(key: String): String {
        val v = value(key)
        val s = when (key) {
            "loss_pct" -> String.format(java.util.Locale.ROOT, "%.1f%%", v)
            "mdd_median_pct", "mdd_worst_pct" -> String.format(java.util.Locale.ROOT, "%.0f%%", v)
            else -> String.format(java.util.Locale.ROOT, "%.2f", v)
        }
        return s.replaceFirst("-", "−")
    }

    /** 표에 쓰는 짧은 이름 */
    val shortName: String get() = name.replace("+변동성", "+vol")

    /**
     * other(보통 ★)와 견준 차이 글자: 금액은 "+0.12억", 손실%·MDD는 "+3.1%p". 같으면 "같음".
     * 음수는 진짜 빼기표(−).
     */
    fun diffText(key: String, other: BacktestRow): String {
        val d = value(key) - other.value(key)
        val pct = key == "loss_pct" || key.startsWith("mdd_")
        val places = if (key == "loss_pct") 1 else if (pct) 0 else 2
        val shown = String.format(java.util.Locale.ROOT, "%.${places}f", kotlin.math.abs(d))
        if (shown.toDouble() == 0.0) return "같음"
        return (if (d > 0) "+" else "−") + shown + if (pct) "%p" else "억"
    }

    /** 이 열에서 other보다 나은가(같으면 null). 손실%는 낮을수록, 나머지는 높을수록(MDD는 0에 가까울수록) 낫다 */
    fun betterThan(col: BacktestColumn, other: BacktestRow): Boolean? {
        val a = value(col.key)
        val b = other.value(col.key)
        if (a == b) return null
        return if (col.higherIsBetter) a > b else a < b
    }
}

/** 열 하나로 줄 세우기. bestFirst면 그 열의 1등부터(higherIsBetter에 맞춰), 아니면 꼴찌부터. 같은 값은 원래 순서 */
fun List<BacktestRow>.sortedByColumn(col: BacktestColumn, bestFirst: Boolean = true): List<BacktestRow> =
    if (col.higherIsBetter == bestFirst) sortedByDescending { it.value(col.key) } else sortedBy { it.value(col.key) }
