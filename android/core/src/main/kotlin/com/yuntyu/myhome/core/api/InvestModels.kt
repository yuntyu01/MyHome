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
