package com.goreecloud.backup

import android.os.Bundle
import android.net.Uri
import androidx.activity.compose.rememberLauncherForActivityResult
import androidx.activity.result.contract.ActivityResultContracts
import androidx.compose.ui.platform.LocalContext
import android.view.WindowManager
import androidx.activity.ComponentActivity
import androidx.activity.compose.setContent
import androidx.compose.foundation.isSystemInDarkTheme
import androidx.compose.foundation.background
import androidx.compose.foundation.layout.*
import androidx.compose.foundation.rememberScrollState
import androidx.compose.foundation.verticalScroll
import androidx.compose.material3.*
import androidx.compose.runtime.*
import androidx.compose.ui.Modifier
import androidx.compose.ui.graphics.Color
import androidx.compose.ui.text.font.FontWeight
import androidx.compose.ui.text.input.PasswordVisualTransformation
import androidx.compose.ui.unit.dp
import androidx.compose.ui.unit.sp
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.launch
import kotlinx.coroutines.withContext
import org.json.JSONObject
import java.net.URL

// Compose reads the active Material3 theme, generated from canonical GC UI tokens.
private val Iris: Color
    @Composable get() = MaterialTheme.colorScheme.primary
private val Canvas: Color
    @Composable get() = MaterialTheme.colorScheme.background
private val Surface: Color
    @Composable get() = MaterialTheme.colorScheme.surface
private val TextColor: Color
    @Composable get() = MaterialTheme.colorScheme.onSurface
private val Soft: Color
    @Composable get() = MaterialTheme.colorScheme.onSurfaceVariant

class MainActivity : ComponentActivity() {
    override fun onCreate(savedInstanceState: Bundle?) {
        super.onCreate(savedInstanceState)
        // Exclude sensitive recovery paths and the access-token input from
        // screenshots and Android's recent-apps thumbnails.
        window.addFlags(WindowManager.LayoutParams.FLAG_SECURE)
        setContent {
            MaterialTheme(
                colorScheme = if (isSystemInDarkTheme()) GoreeCloudUiTheme.Dark else GoreeCloudUiTheme.Light
            ) {
                BackupScreen()
            }
        }
    }
}

@Composable
private fun BackupScreen() {
    var serverInput by remember { mutableStateOf("") }
    var tokenInput by remember { mutableStateOf("") }
    var activeUrl by remember { mutableStateOf<URL?>(null) }
    var activeToken by remember { mutableStateOf("") }
    var overview by remember { mutableStateOf<Overview?>(null) }
    var selectedSnapshot by remember { mutableStateOf("") }
    var verifyConsent by remember { mutableStateOf(false) }
    var restoreConsent by remember { mutableStateOf(false) }
    var uploadConsent by remember { mutableStateOf(false) }
    var selectedDeviceFile by remember { mutableStateOf<Uri?>(null) }
    var resultNotice by remember { mutableStateOf("") }
    var error by remember { mutableStateOf("") }
    var busy by remember { mutableStateOf(false) }
    var session by remember { mutableIntStateOf(0) }
    val scope = rememberCoroutineScope()
    val context = LocalContext.current
    val filePicker = rememberLauncherForActivityResult(ActivityResultContracts.OpenDocument()) { uri ->
        selectedDeviceFile = uri
        uploadConsent = false
    }

    fun connectOrRefresh(base: String, secret: String) {
        if (busy) return
        val url = try { checkedHttpsEndpoint(base) } catch (cause: Exception) {
            error = cause.message ?: "Invalid server URL."
            return
        }
        if (secret.length !in 32..1024) {
            error = "Use a 32+ character access token."
            return
        }
        busy = true
        error = ""
        val generation = session
        scope.launch {
            try {
                val snapshot = withContext(Dispatchers.IO) { fetchOverview(url, secret) }
                if (generation == session) {
                    activeUrl = url
                    activeToken = secret
                    overview = snapshot
                    tokenInput = ""
                    selectedSnapshot = selectedSnapshot.takeIf { id ->
                        snapshot.snapshots.any { it.id == id }
                    }.orEmpty()
                    resultNotice = "Repository status refreshed."
                    verifyConsent = false
                    restoreConsent = false
                    uploadConsent = false
                }
            } catch (cause: Exception) {
                if (generation == session) {
                    error = cause.message ?: "Unable to reach the server."
                    if (activeUrl == null) overview = null
                }
            } finally {
                if (generation == session) busy = false
            }
        }
    }

    fun executeOperation(path: String, body: JSONObject?, description: String) {
        val url = activeUrl ?: return
        if (busy || activeToken.isBlank()) return
        val secret = activeToken
        val generation = session
        busy = true
        error = ""
        resultNotice = "$description in progress. Keep this screen open."
        scope.launch {
            try {
                val response = withContext(Dispatchers.IO) {
                    JSONObject(apiJson(url, path, secret, "POST", body, 30 * 60 * 1000))
                }
                if (generation == session) {
                    resultNotice = when (path) {
                        "/api/v1/verify" -> {
                            require(response.optBoolean("verified", false)) {
                                "The verification result was not confirmed."
                            }
                            "Verified ${response.optLong("filesRead")} unique files. Restore testing is still needed."
                        }
                        "/api/v1/restore" -> {
                            require(response.optBoolean("restored", false)) { "Restore was not confirmed." }
                            val location = response.optString("relativePath").take(240)
                            "Recovered into private server workspace: $location. Inspect files before relying on them."
                        }
                        else -> "Operation finished."
                    }
                    verifyConsent = false
                    restoreConsent = false
                }
            } catch (cause: Exception) {
                if (generation == session) {
                    error = cause.message ?: "Operation failed."
                    resultNotice = "The operation did not complete successfully."
                }
            } finally {
                if (generation == session) busy = false
            }
        }
    }

    fun submitSelectedDeviceFile() {
        val uri = selectedDeviceFile ?: return
        val url = activeUrl ?: return
        if (busy || !uploadConsent || activeToken.isBlank()) return
        val secret = activeToken
        val generation = session
        busy = true
        error = ""
        resultNotice = "Backing up the selected Android document. Do not leave this screen."
        scope.launch {
            try {
                val result = withContext(Dispatchers.IO) {
                    uploadSelectedDocument(context.contentResolver, uri, url, secret)
                }
                if (generation == session) {
                    resultNotice = "Kopia snapshot completed for ${result.optString("name").take(80)} (${result.optLong("bytes")} bytes). Verify and restore-test the backup before relying on it."
                    selectedDeviceFile = null
                    uploadConsent = false
                }
            } catch (cause: Exception) {
                if (generation == session) {
                    error = cause.message ?: "Selected-file backup failed."
                    resultNotice = "Selected-file backup was not confirmed."
                    uploadConsent = false
                }
            } finally {
                if (generation == session) busy = false
            }
        }
    }

    Column(
        modifier = Modifier.fillMaxSize().background(Canvas).verticalScroll(rememberScrollState())
            .padding(horizontal = 24.dp, vertical = 32.dp),
        verticalArrangement = Arrangement.spacedBy(18.dp)
    ) {
        Text("GOREECLOUD  /  BACKUP", fontSize = 12.sp, fontWeight = FontWeight.Bold, color = Iris)
        Text("Private recovery,\non your terms.", fontSize = 32.sp, fontWeight = FontWeight.Bold, lineHeight = 38.sp, color = TextColor)
        Text("Browse encrypted snapshots and control recovery on your own server.", color = Soft)

        Card(colors = CardDefaults.cardColors(containerColor = Surface)) {
            Column(modifier = Modifier.padding(20.dp), verticalArrangement = Arrangement.spacedBy(12.dp)) {
                if (activeUrl == null) {
                    Text("Connect to your server", fontSize = 19.sp, fontWeight = FontWeight.SemiBold)
                    OutlinedTextField(
                        value = serverInput, onValueChange = { serverInput = it.take(512) },
                        singleLine = true, label = { Text("HTTPS server URL") },
                        placeholder = { Text("https://backup.example.com") },
                        modifier = Modifier.fillMaxWidth(), enabled = !busy
                    )
                    OutlinedTextField(
                        value = tokenInput, onValueChange = { tokenInput = it.take(1024) },
                        singleLine = true, visualTransformation = PasswordVisualTransformation(),
                        label = { Text("Access token") }, modifier = Modifier.fillMaxWidth(),
                        enabled = !busy
                    )
                    Button(
                        enabled = !busy && tokenInput.length >= 32,
                        onClick = { connectOrRefresh(serverInput, tokenInput) },
                        modifier = Modifier.fillMaxWidth()
                    ) { Text(if (busy) "Connecting…" else "Connect securely") }
                    Text("Trusted HTTPS only. Your access token is kept in process memory, never saved to disk.", fontSize = 12.sp, color = Soft)
                } else {
                    Text("Connected server", fontSize = 19.sp, fontWeight = FontWeight.SemiBold)
                    Text(activeUrl?.host.orEmpty(), fontSize = 13.sp, color = Soft)
                    Row(horizontalArrangement = Arrangement.spacedBy(10.dp)) {
                        Button(
                            onClick = { connectOrRefresh(activeUrl.toString(), activeToken) },
                            enabled = !busy,
                            modifier = Modifier.weight(1f)
                        ) { Text(if (busy) "Working…" else "Refresh") }
                        OutlinedButton(
                            enabled = !busy,
                            onClick = {
                                session += 1
                                activeUrl = null
                                activeToken = ""
                                tokenInput = ""
                                overview = null
                                selectedSnapshot = ""
                                verifyConsent = false
                                restoreConsent = false
                                uploadConsent = false
                                selectedDeviceFile = null
                                resultNotice = ""
                                error = ""
                            }
                        ) { Text("Disconnect") }
                    }
                }
            }
        }

        val data = overview
        if (data != null) {
            if (data.readOnly) {
                Text(
                    "Read-only session. You can inspect repository status and snapshots but cannot start backups, restores or bandwidth-consuming verification.",
                    color = Soft, fontSize = 13.sp
                )
            }
            Row(horizontalArrangement = Arrangement.spacedBy(10.dp), modifier = Modifier.fillMaxWidth()) {
                Box(modifier = Modifier.weight(1f)) {
                    MetricCard("Repository", if (data.repositoryConnected) "Online" else "Not connected")
                }
                Box(modifier = Modifier.weight(1f)) {
                    MetricCard("Snapshots", data.snapshots.size.toString())
                }
            }
            MetricCard("Rclone remotes", data.remotesCount?.toString() ?: "Unavailable")
            data.notes.forEach { note ->
                Text(note, fontSize = 12.sp, color = Soft)
            }
            Card(colors = CardDefaults.cardColors(containerColor = Surface)) {
                Column(modifier = Modifier.padding(18.dp), verticalArrangement = Arrangement.spacedBy(12.dp)) {
                    Text("Back up a selected Android file", fontSize = 20.sp, fontWeight = FontWeight.SemiBold)
                    Text(
                        if (data.mobileUploadEnabled) "Choose one document with Android's system picker. It is streamed over trusted HTTPS, snapshotted by Kopia and removed from temporary server staging. Maximum 32 MiB per selection."
                        else if (data.readOnly) "This access token cannot upload files."
                        else "Selected-device-file backup is disabled until the server operator configures a private GCB_MOBILE_INGEST_ROOT.",
                        color = Soft, fontSize = 13.sp
                    )
                    if (data.mobileUploadEnabled) {
                        OutlinedButton(
                            enabled = !busy,
                            onClick = { filePicker.launch(arrayOf("*/*")) }
                        ) { Text(if (selectedDeviceFile == null) "Choose Android document" else "Choose different document") }
                        if (selectedDeviceFile != null) {
                            Text("One document selected. Its local URI is not stored or displayed.", fontSize = 12.sp, color = Soft)
                            ConsentRow(
                                "I authorize uploading this one selected file to my configured backup server and understand network costs.",
                                uploadConsent, !busy, { uploadConsent = it }
                            )
                            Button(
                                enabled = !busy && data.repositoryConnected && uploadConsent,
                                onClick = { submitSelectedDeviceFile() }
                            ) { Text("Back up selected file") }
                        }
                    }
                }
            }

            Card(colors = CardDefaults.cardColors(containerColor = Surface)) {
                Column(modifier = Modifier.padding(18.dp), verticalArrangement = Arrangement.spacedBy(12.dp)) {
                    Text("Recovery points", fontSize = 20.sp, fontWeight = FontWeight.SemiBold)
                    Text("Showing up to 100 recent snapshot records. Select one for contained restoration.", color = Soft, fontSize = 12.sp)
                    if (data.snapshots.isEmpty()) {
                        Text("No snapshot details available.", color = Soft)
                    } else {
                        data.snapshots.forEach { item ->
                            val selected = selectedSnapshot == item.id
                            OutlinedCard(onClick = {
                                selectedSnapshot = item.id
                                restoreConsent = false
                            }) {
                                Row(modifier = Modifier.fillMaxWidth().padding(12.dp), horizontalArrangement = Arrangement.spacedBy(8.dp)) {
                                    RadioButton(
                                        selected = selected,
                                        onClick = {
                                            selectedSnapshot = item.id
                                            restoreConsent = false
                                        }
                                    )
                                    Column(verticalArrangement = Arrangement.spacedBy(3.dp)) {
                                        Text(item.source, fontWeight = FontWeight.SemiBold)
                                        if (item.timestamp.isNotBlank()) Text(item.timestamp, fontSize = 12.sp, color = Soft)
                                        Text(item.id.take(20) + "…", fontSize = 11.sp, color = Soft)
                                    }
                                }
                            }
                        }
                    }
                }
            }

            Card(colors = CardDefaults.cardColors(containerColor = Surface)) {
                Column(modifier = Modifier.padding(18.dp), verticalArrangement = Arrangement.spacedBy(12.dp)) {
                    Text("Verify archive integrity", fontSize = 20.sp, fontWeight = FontWeight.SemiBold)
                    Text("Reads 100% of stored backup file contents. Cloud storage may charge bandwidth fees; this does not replace a restore drill.", fontSize = 13.sp, color = Soft)
                    ConsentRow(
                        "I understand this may read cloud data and incur costs.",
                        verifyConsent, !busy && !data.readOnly, { verifyConsent = it }
                    )
                    Button(
                        enabled = !busy && data.repositoryConnected && verifyConsent && !data.readOnly,
                        onClick = { executeOperation("/api/v1/verify", null, "Archive verification") }
                    ) { Text("Verify all snapshot files") }
                }
            }

            Card(colors = CardDefaults.cardColors(containerColor = Surface)) {
                Column(modifier = Modifier.padding(18.dp), verticalArrangement = Arrangement.spacedBy(12.dp)) {
                    Text("Contained restore", fontSize = 20.sp, fontWeight = FontWeight.SemiBold)
                    Text(
                        if (data.restoreEnabled) "Restores the selected snapshot into a new private server workspace. Original files are not overwritten."
                        else if (data.readOnly) "This access token cannot restore files."
                        else "Server-side restoration is disabled until the operator configures GCB_RESTORE_ROOT.",
                        fontSize = 13.sp, color = Soft
                    )
                    if (data.restoreEnabled) {
                        ConsentRow(
                            "I understand restoration uses server storage and may incur download costs.",
                            restoreConsent, !busy && selectedSnapshot.isNotEmpty(),
                            { restoreConsent = it }
                        )
                        Button(
                            enabled = !busy && data.repositoryConnected && selectedSnapshot.isNotBlank() && restoreConsent,
                            onClick = {
                                val payload = JSONObject()
                                    .put("snapshotId", selectedSnapshot)
                                    .put("acknowledge", true)
                                executeOperation("/api/v1/restore", payload, "Contained restore")
                            }
                        ) { Text("Restore selected snapshot") }
                    }
                }
            }
            Text(
                "Selected-file foreground backup is available only when enabled by your server. Automatic photo/gallery backup, bulk device backup, unattended scheduling and background sync are not implemented.",
                color = Soft, fontSize = 12.sp
            )
        }

        if (busy) LinearProgressIndicator(modifier = Modifier.fillMaxWidth())
        if (resultNotice.isNotBlank()) Text(resultNotice, color = Soft, fontSize = 13.sp)
        if (error.isNotBlank()) Text(error, color = MaterialTheme.colorScheme.error, fontSize = 13.sp)
        Text("GoreeShield privacy · Wardveil security · GoreeCloud UI", color = Soft, fontSize = 12.sp)
    }
}

@Composable
private fun ConsentRow(label: String, checked: Boolean, enabled: Boolean, onChange: (Boolean) -> Unit) {
    Row(horizontalArrangement = Arrangement.spacedBy(8.dp)) {
        Checkbox(checked = checked, enabled = enabled, onCheckedChange = onChange)
        Text(label, fontSize = 13.sp, color = Soft, modifier = Modifier.padding(top = 12.dp))
    }
}

@Composable
private fun MetricCard(label: String, value: String) {
    Card(colors = CardDefaults.cardColors(containerColor = Surface), modifier = Modifier.fillMaxWidth()) {
        Column(modifier = Modifier.padding(18.dp)) {
            Text(label, color = Soft, fontSize = 12.sp)
            Text(value, color = TextColor, fontWeight = FontWeight.Bold, fontSize = 22.sp)
        }
    }
}
