package com.goreecloud.backup

import android.os.Bundle
import androidx.activity.ComponentActivity
import androidx.activity.compose.setContent
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
import org.json.JSONArray
import org.json.JSONObject
import java.net.HttpURLConnection
import java.net.URL

private val Iris = Color(0xFF5146C8)
private val Canvas = Color(0xFFF6F7FB)
private val Surface = Color(0xFFFFFFFF)
private val TextColor = Color(0xFF1B2030)
private val Soft = Color(0xFF505B70)

class MainActivity : ComponentActivity() {
    override fun onCreate(savedInstanceState: Bundle?) {
        super.onCreate(savedInstanceState)
        setContent {
            MaterialTheme(colorScheme = lightColorScheme(primary = Iris, surface = Surface, onSurface = TextColor, background = Canvas)) {
                BackupScreen()
            }
        }
    }
}

private fun fetchJson(base: String, path: String, token: String): String {
    val url = URL(base.trimEnd('/') + path)
    require(url.protocol == "https") { "A trusted HTTPS endpoint is required." }
    val connection = url.openConnection() as HttpURLConnection
    try {
        connection.requestMethod = "GET"
        connection.setRequestProperty("Authorization", "Bearer $token")
        connection.setRequestProperty("Accept", "application/json")
        connection.connectTimeout = 8_000
        connection.readTimeout = 15_000
        connection.instanceFollowRedirects = false
        val code = connection.responseCode
        if (code != 200) throw IllegalStateException("Request failed (HTTP $code)")
        val data = connection.inputStream.bufferedReader().use { it.readText().take(1_048_576) }
        return data
    } finally {
        connection.disconnect()
    }
}

@Composable
private fun BackupScreen() {
    var server by remember { mutableStateOf("") }
    var token by remember { mutableStateOf("") } // Not saved to disk or restored in saved instance state.
    var connected by remember { mutableStateOf(false) }
    var repository by remember { mutableStateOf("Not checked") }
    var snapshots by remember { mutableIntStateOf(0) }
    var remotes by remember { mutableIntStateOf(0) }
    var error by remember { mutableStateOf("") }
    var busy by remember { mutableStateOf(false) }
    val scope = rememberCoroutineScope()

    Column(
        modifier = Modifier.fillMaxSize().background(Canvas).verticalScroll(rememberScrollState())
            .padding(horizontal = 24.dp, vertical = 36.dp),
        verticalArrangement = Arrangement.spacedBy(20.dp)
    ) {
        Text("GOREECLOUD  /  BACKUP", fontSize = 12.sp, fontWeight = FontWeight.Bold, color = Iris)
        Text("Your backups,\nat a glance.", fontSize = 34.sp, fontWeight = FontWeight.Bold, lineHeight = 38.sp, color = TextColor)
        Text("Private, encrypted repository status from your own server.", color = Soft)
        Card(colors = CardDefaults.cardColors(containerColor = Surface)) {
            Column(modifier = Modifier.padding(20.dp), verticalArrangement = Arrangement.spacedBy(14.dp)) {
                Text("Connect to your server", fontSize = 19.sp, fontWeight = FontWeight.SemiBold)
                OutlinedTextField(
                    value = server, onValueChange = { server = it.take(512) },
                    singleLine = true, label = { Text("HTTPS server URL") },
                    placeholder = { Text("https://backup.example.com") }, modifier = Modifier.fillMaxWidth()
                )
                OutlinedTextField(
                    value = token, onValueChange = { token = it.take(1024) },
                    singleLine = true, visualTransformation = PasswordVisualTransformation(),
                    label = { Text("Access token") }, modifier = Modifier.fillMaxWidth()
                )
                Button(
                    enabled = !busy && token.length >= 32 && server.startsWith("https://"),
                    onClick = {
                        busy = true
                        error = ""
                        scope.launch {
                            try {
                                val result = withContext(Dispatchers.IO) {
                                    val s = JSONObject(fetchJson(server, "/api/v1/status", token))
                                    val shots = JSONArray(fetchJson(server, "/api/v1/snapshots", token))
                                    val destinations = JSONObject(fetchJson(server, "/api/v1/remotes", token))
                                    Triple(s.optBoolean("connected", false), shots.length(),
                                        destinations.optJSONArray("remotes")?.length() ?: 0)
                                }
                                repository = if (result.first) "Connected" else "Not connected"
                                snapshots = result.second
                                remotes = result.third
                                connected = true
                            } catch (cause: Exception) {
                                connected = false
                                error = cause.message ?: "Unable to reach the server"
                            } finally { busy = false }
                        }
                    },
                    modifier = Modifier.fillMaxWidth()
                ) { Text(if (busy) "Checking…" else "Connect and refresh") }
                Text("Token is held in memory only. Trusted HTTPS is required.", fontSize = 12.sp, color = Soft)
            }
        }
        if (connected) {
            MetricCard("Kopia repository", repository)
            MetricCard("Available snapshots", snapshots.toString())
            MetricCard("Rclone destinations", remotes.toString())
            Text("This is a read-only companion. Device backups, restore, scheduling and background sync are not implemented.", color = Soft, fontSize = 12.sp)
        }
        if (error.isNotEmpty()) {
            Text(error, color = MaterialTheme.colorScheme.error)
        }
        Text("GoreeShield privacy · Wardveil security", color = Soft, fontSize = 12.sp)
    }
}

@Composable
private fun MetricCard(label: String, value: String) {
    Card(colors = CardDefaults.cardColors(containerColor = Surface), modifier = Modifier.fillMaxWidth()) {
        Column(modifier = Modifier.padding(20.dp)) {
            Text(label, color = Soft, fontSize = 13.sp)
            Text(value, color = TextColor, fontWeight = FontWeight.Bold, fontSize = 27.sp)
        }
    }
}
