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
                    val f = saveShot(ctx, bmp)
                    if (f != null) deliver(f) else Toast.makeText(ctx, "사진을 저장하지 못했어요", Toast.LENGTH_SHORT).show()
                }
            },
        )
    }
    return remember { { if (cameraGranted(ctx)) open = true else ask.launch(Manifest.permission.CAMERA) } }
}

/** 화면 안 무음 카메라 손잡이: 지금 미리보기 한 장 뜨기(shot), 손전등(torch) */
class CameraShooter {
    internal var view: PreviewView? = null
    internal var camera: androidx.camera.core.Camera? = null
    /** 미리보기가 떴는지(셔터를 눌러도 되는지) */
    var ready by mutableStateOf(false)
        internal set
    var hasTorch by mutableStateOf(false)
        internal set
    var error by mutableStateOf<String?>(null)
        internal set

    fun shot(): Bitmap? = view?.bitmap

    fun torch(on: Boolean) {
        runCatching { camera?.cameraControl?.enableTorch(on) }
    }
}

/** 미리보기 한 장을 끼니 사진 임시 jpg로 저장한다(실패하면 null) */
suspend fun saveShot(ctx: Context, bmp: Bitmap): File? = withContext(Dispatchers.IO) {
    val (file, _) = MealPhotos.newCapture(ctx)
    val ok = runCatching { file.outputStream().use { bmp.compress(Bitmap.CompressFormat.JPEG, 92, it) } }.getOrDefault(false)
    if (ok && file.length() > 0) file else { file.delete(); null }
}

/**
 * 화면 안에 박는 무음 카메라(끼니 촬영 화면). 권한이 없으면 그 자리에서 묻고, 거절하면 안내와 다시 묻기 버튼.
 * fill이면 칸을 가득 채우게 잘라서 보여 준다(찍히는 것도 보이는 그대로).
 */
@Composable
fun SilentCameraView(shooter: CameraShooter, modifier: Modifier = Modifier, fill: Boolean = true) {
    val ctx = LocalContext.current
    var granted by remember { mutableStateOf(cameraGranted(ctx)) }
    var asked by rememberSaveable { mutableStateOf(false) }
    val ask = rememberLauncherForActivityResult(ActivityResultContracts.RequestPermission()) { granted = it; asked = true }
    LaunchedEffect(Unit) { if (!granted && !asked) ask.launch(Manifest.permission.CAMERA) }
    Box(modifier.background(Color.Black), contentAlignment = Alignment.Center) {
        if (granted) {
            CameraPreview(shooter, Modifier.fillMaxSize(), fill)
            shooter.error?.let { Text(it, color = Color.White) }
        } else if (asked) {
            androidx.compose.foundation.layout.Column(horizontalAlignment = Alignment.CenterHorizontally) {
                Text("카메라를 허용하면 바로 찍어요", color = Color.White)
                androidx.compose.material3.TextButton(onClick = { ask.launch(Manifest.permission.CAMERA) }) { Text("허용하기", color = Color.White) }
            }
        }
    }
}

/** CameraX 미리보기(촬영 요청은 보내지 않는다). 붙으면 shooter.ready */
@Composable
private fun CameraPreview(shooter: CameraShooter, modifier: Modifier, fill: Boolean) {
    val ctx = LocalContext.current
    val owner = androidx.lifecycle.compose.LocalLifecycleOwner.current
    val view = remember {
        PreviewView(ctx).apply {
            // TextureView: 대화상자 창 안에서도 그려지고, bitmap으로 화면 그대로 뜰 수 있다
            implementationMode = PreviewView.ImplementationMode.COMPATIBLE
            scaleType = if (fill) PreviewView.ScaleType.FILL_CENTER else PreviewView.ScaleType.FIT_CENTER
        }
    }
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
                val cam = p.bindToLifecycle(owner, CameraSelector.DEFAULT_BACK_CAMERA, preview)
                shooter.view = view
                shooter.camera = cam
                shooter.hasTorch = cam.cameraInfo.hasFlashUnit()
                shooter.error = null
                shooter.ready = true
            }.onFailure { shooter.error = "카메라를 열지 못했어요" }
        }, ContextCompat.getMainExecutor(ctx))
        onDispose {
            shooter.ready = false
            shooter.view = null
            shooter.camera = null
            runCatching { provider?.unbindAll() }
        }
    }
    AndroidView({ view }, modifier)
}

/** 화면 가득 카메라 미리보기 + 닫기 + 셔터(헬스 사진 인증). 셔터를 누르면 지금 미리보기 한 장을 넘긴다 */
@Composable
private fun SilentCameraDialog(onDismiss: () -> Unit, onShot: (Bitmap) -> Unit) {
    Dialog(onDismissRequest = onDismiss, properties = DialogProperties(usePlatformDefaultWidth = false, decorFitsSystemWindows = false)) {
        val ctx = LocalContext.current
        val shooter = remember { CameraShooter() }
        var flash by remember { mutableStateOf(false) }
        LaunchedEffect(flash) { if (flash) { delay(120); flash = false } }
        // 대화상자는 다른 창이라 바깥 글자 고르기와 엮이지 않게 끈다(Selection.kt)
        DisableSelection {
            Box(Modifier.fillMaxSize().background(Color.Black)) {
                CameraPreview(shooter, Modifier.fillMaxSize(), fill = false)
                if (flash) Box(Modifier.fillMaxSize().background(Color.White.copy(alpha = 0.5f)))
                IconButton(onClick = onDismiss, modifier = Modifier.align(Alignment.TopStart).statusBarsPadding().padding(8.dp)) {
                    Icon(CloseIcon, contentDescription = "닫기", tint = Color.White)
                }
                shooter.error?.let { Text(it, Modifier.align(Alignment.Center), color = Color.White) }
                ShutterButton(
                    enabled = shooter.ready,
                    modifier = Modifier.align(Alignment.BottomCenter).navigationBarsPadding().padding(bottom = 36.dp),
                ) {
                    val bmp = shooter.shot()
                    if (bmp == null) {
                        Toast.makeText(ctx, "아직 화면이 안 떴어요. 다시 눌러 주세요", Toast.LENGTH_SHORT).show()
                    } else {
                        flash = true
                        onShot(bmp)
                    }
                }
            }
        }
    }
}

/** 셔터: 흰 테두리 원 안에 흰 원 */
@Composable
fun ShutterButton(enabled: Boolean, modifier: Modifier = Modifier, ring: Color = Color.White, onClick: () -> Unit) {
    Box(
        modifier.size(76.dp).clip(CircleShape).border(4.dp, ring, CircleShape).padding(8.dp).clip(CircleShape)
            .background(if (enabled) ring else ring.copy(alpha = 0.4f))
            .tap(enabled = enabled, onClickLabel = "찍기", role = Role.Button, onClick = onClick),
    )
}
