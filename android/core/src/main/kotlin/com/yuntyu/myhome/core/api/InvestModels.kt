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

/** GET /v1/invest/alerts 응답. 자동매매 작업(systemd)이 실패할 때만 한 줄씩 생긴다. */
@Serializable
data class InvestAlerts(val alerts: List<InvestAlert> = emptyList())

@Serializable
data class InvestAlert(
    val id: Long,
    @SerialName("created_at") val createdAt: String = "",
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
}
