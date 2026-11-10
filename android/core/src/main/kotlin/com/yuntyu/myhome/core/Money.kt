package com.yuntyu.myhome.core

import kotlin.math.absoluteValue

/** 금액은 원 단위 정수(Long)로만 다룬다. 부동소수점 금지. */
object Money {
    /** 12345 → "12,345원", -500 → "-500원" */
    fun won(amount: Long): String = (if (amount < 0) "-" else "") + grouped(amount.absoluteValue) + "원"

    /** 12345 → "12,345" */
    fun grouped(amount: Long): String {
        val digits = amount.absoluteValue.toString()
        val sb = StringBuilder()
        digits.forEachIndexed { i, c ->
            if (i > 0 && (digits.length - i) % 3 == 0) sb.append(',')
            sb.append(c)
        }
        return (if (amount < 0) "-" else "") + sb.toString()
    }

    /**
     * 읽기 쉬운 한국식 금액(금액 칸 위에 보여 준다): 500000 → "50만원", 12345000 → "1,234만 5,000원",
     * 120000000 → "1억 2,000만원", 3000 → "3,000원". 버리는 자리 없이 정확하다.
     */
    fun korean(amount: Long): String {
        val abs = amount.absoluteValue
        if (abs == 0L) return "0원"
        val parts = listOfNotNull(
            (abs / 100_000_000).takeIf { it > 0 }?.let { grouped(it) + "억" },
            ((abs % 100_000_000) / 10_000).takeIf { it > 0 }?.let { grouped(it) + "만" },
            (abs % 10_000).takeIf { it > 0 }?.let { grouped(it) },
        )
        return (if (amount < 0) "-" else "") + parts.joinToString(" ") + "원"
    }

    /** 위젯처럼 좁은 곳용: 1234567 → "123만", 9800 → "9,800" */
    fun compact(amount: Long): String {
        val abs = amount.absoluteValue
        val sign = if (amount < 0) "-" else ""
        return when {
            abs >= 100_000_000 -> sign + grouped(abs / 100_000_000) + "억" +
                ((abs % 100_000_000) / 10_000).let { if (it > 0) " ${grouped(it)}만" else "" }
            abs >= 100_000 -> sign + grouped(abs / 10_000) + "만"
            else -> sign + grouped(abs)
        }
    }
}
