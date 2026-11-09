package com.yuntyu.myhome.core.api

import kotlinx.serialization.Serializable

/*
 * 집중 타이머 동기화(서버 /v1/focus). 앱이 원본(Room)이고 서버는 보관·웹용이다.
 * id는 앱이 만든 UUID, 시각은 RFC3339. 같은 PUT을 여러 번 보내도 한 줄이고, 서버에 더 새 것(updatedAt)이 있으면 서버 것을 둔다.
 * 장소는 이름만 보낸다(좌표는 폰에만).
 */

@Serializable
data class FocusSubjectBody(val name: String, val color: String, val sort: Int, val archived: Boolean, val updatedAt: String)

@Serializable
data class FocusSessionBody(val subjectId: String, val startedAt: String, val endedAt: String, val deleted: Boolean, val updatedAt: String)

@Serializable
data class FocusStayBody(val place: String, val enteredAt: String, val exitedAt: String, val deleted: Boolean, val updatedAt: String)

@Serializable
data class FocusSubjectDto(
    val id: String,
    val name: String,
    val color: String,
    val sort: Int = 0,
    val archived: Boolean = false,
    val updatedAt: String,
)

@Serializable
data class FocusSessionDto(
    val id: String,
    val subjectId: String,
    val startedAt: String,
    val endedAt: String,
    val deleted: Boolean = false,
    val updatedAt: String,
)

@Serializable
data class FocusStayDto(
    val id: String,
    val place: String,
    val enteredAt: String,
    val exitedAt: String,
    val deleted: Boolean = false,
    val updatedAt: String,
)

/** GET /v1/focus/all: 새로 깐 앱이 되살릴 때 쓴다(지운·숨긴 것 포함) */
@Serializable
data class FocusAll(
    val subjects: List<FocusSubjectDto> = emptyList(),
    val sessions: List<FocusSessionDto> = emptyList(),
    val stays: List<FocusStayDto> = emptyList(),
)
