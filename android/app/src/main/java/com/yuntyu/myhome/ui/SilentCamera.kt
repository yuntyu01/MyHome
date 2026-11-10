package com.yuntyu.myhome.ui

import android.Manifest
import android.content.Context
import android.content.pm.PackageManager
import android.graphics.Bitmap
import android.widget.Toast
import androidx.activity.compose.rememberLauncherForActivityResult
import androidx.activity.result.contract.ActivityResultContracts
import androidx.camera.core.CameraSelector
import androidx.camera.core.Preview
import androidx.camera.core.resolutionselector.AspectRatioStrategy
import androidx.camera.core.resolutionselector.ResolutionSelector
import androidx.camera.lifecycle.ProcessCameraProvider
import androidx.camera.view.PreviewView
import androidx.compose.foundation.background
import androidx.compose.foundation.border
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.navigationBarsPadding
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.size
import androidx.compose.foundation.layout.statusBarsPadding
import androidx.compose.foundation.shape.CircleShape
import androidx.compose.foundation.text.selection.DisableSelection
import androidx.compose.material3.Icon
import androidx.compose.material3.IconButton
import androidx.compose.material3.Text
import androidx.compose.runtime.Composable
import androidx.compose.runtime.DisposableEffect
import androidx.compose.runtime.LaunchedEffect
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.rememberCoroutineScope
import androidx.compose.runtime.rememberUpdatedState
import androidx.compose.runtime.saveable.rememberSaveable
import androidx.compose.runtime.setValue
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.draw.clip
import androidx.compose.ui.graphics.Color
import androidx.compose.ui.platform.LocalContext
import androidx.compose.ui.semantics.Role
import androidx.compose.ui.unit.dp
import androidx.compose.ui.viewinterop.AndroidView
import androidx.compose.ui.window.Dialog
import androidx.compose.ui.window.DialogProperties
import androidx.core.content.ContextCompat
import com.yuntyu.myhome.health.MealPhotos
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.delay
import kotlinx.coroutines.launch
import kotlinx.coroutines.withContext
import java.io.File

/*
 * 소리 없이 사진 찍기(사용자 요청, 2026-11-10).
 * 기본 카메라 앱(ACTION_IMAGE_CAPTURE)은 한국 폰에서 셔터 소리를 끌 수 없어서, 앱 안에 카메라 미리보기를 띄우고
 * 셔터를 누르면 미리보기 화면 한 장(PreviewView.bitmap)을 JPEG로 저장한다. 카메라에 촬영 요청을 보내지 않으므로
 * 어느 폰에서도 셔터 소리가 나지 않는다. 대신 해상도는 미리보기 크기(보통 긴 변 1440~1920px)인데,
 * 서버로 보내는 사진은 어차피 긴 변 1024px로 줄이므로(MealPhotos.downscaled) 모자라지 않다.
 * 카메라 권한을 거절하면 예전처럼 기본 카메라 앱으로 찍는다(소리가 날 수 있다고 알린다).
 */

private fun cameraGranted(ctx: Context) =
    ContextCompat.checkSelfPermission(ctx, Manifest.permission.CAMERA) == PackageManager.PERMISSION_GRANTED

/**
 * 찍기 함수를 돌려준다. 부르면 무음 카메라가 열리고, 찍으면 onPhoto(임시 jpg 파일)를 부른다(닫으면 부르지 않는다).
 * 파일은 MealPhotos의 임시 파일이라 다 쓰면 부르는 쪽이 지우거나 옮긴다.
 */
@Composable
fun rememberSilentCamera(onPhoto: (File) -> Unit): () -> Unit {
    val ctx = LocalContext.current
    val scope = rememberCoroutineScope()
    val deliver by rememberUpdatedState(onPhoto)
    var open by remember { mutableStateOf(false) }
    // 권한이 없을 때 쓰는 기본 카메라(앱이 죽었다 살아나도 파일을 찾게 경로를 저장)
    var pending by rememberSaveable { mutableStateOf<String?>(null) }
    val system = rememberLauncherForActivityResult(ActivityResultContracts.TakePicture()) { ok ->
        val f = pending?.let(::File)
        pending = null
        if (ok && f != null && f.length() > 0) deliver(f) else f?.delete()
    }
    val ask = rememberLauncherForActivityResult(ActivityResultContracts.RequestPermission()) { granted ->
        if (granted) {
            open = true
        } else {
            Toast.makeText(ctx, "카메라 권한이 없어서 기본 카메라로 찍어요(소리가 날 수 있어요)", Toast.LENGTH_LONG).show()
            val (f, uri) = MealPhotos.newCapture(ctx)
            pending = f.path
            system.launch(uri)
        }
    }
    if (open) {
        SilentCameraDialog(
            onDismiss = { open = false },
            onShot = { bmp ->
                open = false
                scope.launch {
                    val f = withContext(Dispatchers.IO) {
                        val (file, _) = MealPhotos.newCapture(ctx)
                        val ok = runCatching { file.outputStream().use { bmp.compress(Bitmap.CompressFormat.JPEG, 92, it) } }.getOrDefault(false)
                        if (ok && file.length() > 0) file else { file.delete(); null }
                    }
                    if (f != null) deliver(f) else Toast.makeText(ctx, "사진을 저장하지 못했어요", Toast.LENGTH_SHORT).show()
                }
            },
        )
    }
    return remember { { if (cameraGranted(ctx)) open = true else ask.launch(Manifest.permission.CAMERA) } }
}

/** 화면 가득 카메라 미리보기 + 닫기 + 셔터. 셔터를 누르면 지금 미리보기 한 장을 넘긴다 */
@Composable
private fun SilentCameraDialog(onDismiss: () -> Unit, onShot: (Bitmap) -> Unit) {
    Dialog(onDismissRequest = onDismiss, properties = DialogProperties(usePlatformDefaultWidth = false, decorFitsSystemWindows = false)) {
        val ctx = LocalContext.current
        val owner = androidx.lifecycle.compose.LocalLifecycleOwner.current
        val view = remember {
            PreviewView(ctx).apply {
                // TextureView: 대화상자 창 안에서도 그려지고, bitmap으로 화면 그대로 뜰 수 있다
                implementationMode = PreviewView.ImplementationMode.COMPATIBLE
                scaleType = PreviewView.ScaleType.FIT_CENTER
            }
        }
        var ready by remember { mutableStateOf(false) }
        var error by remember { mutableStateOf<String?>(null) }
        var flash by remember { mutableStateOf(false) }
        DisposableEffect(owner) {
            val future = ProcessCameraProvider.getInstance(ctx)
            var provider: ProcessCameraProvider? = null
            future.addListener({
                runCatching {
                    val p = future.get()
                    provider = p
                    val preview = Preview.Builder()
                        .setResolutionSelector(ResolutionSelector.Builder().setAspectRatioStrategy(AspectRatioStrategy.RATIO_4_3_FALLBACK_AUTO_STRATEGY).build())
                        .build()
                    preview.setSurfaceProvider(view.surfaceProvider)
                    p.unbindAll()
                    p.bindToLifecycle(owner, CameraSelector.DEFAULT_BACK_CAMERA, preview)
                    ready = true
                }.onFailure { error = "카메라를 열지 못했어요" }
            }, ContextCompat.getMainExecutor(ctx))
            onDispose { runCatching { provider?.unbindAll() } }
        }
        LaunchedEffect(flash) { if (flash) { delay(120); flash = false } }
        // 대화상자는 다른 창이라 바깥 글자 고르기와 엮이지 않게 끈다(Selection.kt)
        DisableSelection {
            Box(Modifier.fillMaxSize().background(Color.Black)) {
                AndroidView({ view }, Modifier.fillMaxSize())
                if (flash) Box(Modifier.fillMaxSize().background(Color.White.copy(alpha = 0.5f)))
                IconButton(onClick = onDismiss, modifier = Modifier.align(Alignment.TopStart).statusBarsPadding().padding(8.dp)) {
                    Icon(CloseIcon, contentDescription = "닫기", tint = Color.White)
                }
                error?.let { Text(it, Modifier.align(Alignment.Center), color = Color.White) }
                Box(
                    Modifier.align(Alignment.BottomCenter).navigationBarsPadding().padding(bottom = 36.dp)
                        .size(76.dp).clip(CircleShape).border(4.dp, Color.White, CircleShape).padding(8.dp).clip(CircleShape)
                        .background(if (ready) Color.White else Color.White.copy(alpha = 0.4f))
                        .tap(enabled = ready, onClickLabel = "찍기", role = Role.Button) {
                            val bmp = view.bitmap
                            if (bmp == null) {
                                Toast.makeText(ctx, "아직 화면이 안 떴어요. 다시 눌러 주세요", Toast.LENGTH_SHORT).show()
                            } else {
                                flash = true
                                onShot(bmp)
                            }
                        },
                )
            }
        }
    }
}
