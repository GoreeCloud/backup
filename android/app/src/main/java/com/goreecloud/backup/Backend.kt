package com.goreecloud.backup

import org.json.JSONArray
import org.json.JSONObject
import java.io.ByteArrayOutputStream
import java.net.HttpURLConnection
import java.net.URI
import java.net.URL

/** Never send the token to an untrusted hostname, userinfo, redirected URL or plaintext endpoint. */
internal fun checkedHttpsEndpoint(raw: String): URL {
    require(raw.isNotBlank() && raw.length <= 512) { "Enter an HTTPS server URL." }
    val uri = try { URI(raw.trim()) } catch (_: Exception) {
        throw IllegalArgumentException("Invalid server URL.")
    }
    require(
        uri.scheme.equals("https", ignoreCase = true) &&
            uri.host != null && uri.host.isNotBlank() &&
            uri.rawUserInfo == null && uri.rawQuery == null && uri.rawFragment == null &&
            (uri.rawPath.isNullOrEmpty() || uri.rawPath == "/") &&
            (uri.port == -1 || uri.port in 1..65535)
    ) { "Use an HTTPS URL with a host and no path, username, query or fragment." }
    return URL(uri.toString().trimEnd('/'))
}

private fun boundedResponse(connection: HttpURLConnection): String {
    val output = ByteArrayOutputStream()
    val buffer = ByteArray(8192)
    connection.inputStream.use { input ->
        while (true) {
            val count = input.read(buffer)
            if (count < 0) break
            if (output.size() + count > 1_048_576) {
                throw IllegalStateException("Server response exceeds the 1 MiB mobile limit.")
            }
            output.write(buffer, 0, count)
        }
    }
    return output.toString(Charsets.UTF_8.name())
}

internal fun apiJson(
    server: URL,
    path: String,
    token: String,
    method: String = "GET",
    body: JSONObject? = null,
    timeoutMs: Int = 15_000
): String {
    require(path.startsWith("/api/v1/") && !path.contains("..")) { "Invalid API path." }
    require(token.length in 32..1024 && !token.any { it.isWhitespace() || it == '\u0000' }) {
        "Access token is invalid."
    }
    require(method == "GET" || method == "POST") { "Unsupported request method." }
    val requestUrl = URL(server.toString().trimEnd('/') + path)
    require(requestUrl.protocol == "https" && requestUrl.host == server.host &&
        requestUrl.port == server.port) { "Request host changed unexpectedly." }
    val connection = requestUrl.openConnection() as HttpURLConnection
    try {
        connection.instanceFollowRedirects = false
        connection.requestMethod = method
        connection.setRequestProperty("Authorization", "Bearer $token")
        connection.setRequestProperty("Accept", "application/json")
        connection.connectTimeout = 8_000
        connection.readTimeout = timeoutMs
        connection.useCaches = false
        if (body != null) {
            require(method == "POST") { "GET cannot contain a request body." }
            val bytes = body.toString().toByteArray(Charsets.UTF_8)
            require(bytes.size <= 4096) { "Request is too large." }
            connection.setRequestProperty("Content-Type", "application/json")
            connection.doOutput = true
            connection.setFixedLengthStreamingMode(bytes.size)
            connection.outputStream.use { it.write(bytes) }
        }
        val status = connection.responseCode
        if (status !in 200..299) throw IllegalStateException("Server returned HTTP $status.")
        return boundedResponse(connection)
    } finally {
        connection.disconnect()
    }
}

internal data class SnapshotSummary(val id: String, val source: String, val timestamp: String)
internal data class Overview(
    val repositoryConnected: Boolean,
    val snapshots: List<SnapshotSummary>,
    val remotesCount: Int?,
    val restoreEnabled: Boolean,
    val notes: List<String>
)

internal fun fetchOverview(server: URL, token: String): Overview {
    val status = JSONObject(apiJson(server, "/api/v1/status", token))
    val notes = mutableListOf<String>()
    val snapshots = try {
        val raw = JSONArray(apiJson(server, "/api/v1/snapshots", token))
        val rows = mutableListOf<SnapshotSummary>()
        val start = (raw.length() - 100).coerceAtLeast(0)
        for (index in raw.length() - 1 downTo start) {
            val item = raw.optJSONObject(index) ?: continue
            val id = item.optString("id")
            if (!Regex("^[A-Za-z0-9_-]{8,128}$").matches(id)) continue
            val sourcePath = item.optJSONObject("source")?.optString("path").orEmpty()
            val label = sourcePath.trimEnd('/').substringAfterLast('/').ifBlank { "Snapshot" }
            rows += SnapshotSummary(id, label.take(80), item.optString("startTime").take(60))
        }
        rows
    } catch (_: Exception) {
        notes += "Snapshot listing unavailable; check repository access."
        emptyList()
    }
    val remotes = try {
        JSONObject(apiJson(server, "/api/v1/remotes", token))
            .optJSONArray("remotes")?.length() ?: 0
    } catch (_: Exception) {
        notes += "Rclone remotes are unavailable."
        null
    }
    val canRestore = try {
        JSONObject(apiJson(server, "/api/v1/info", token)).optBoolean("restoreEnabled", false)
    } catch (_: Exception) {
        notes += "Restore capability could not be checked."
        false
    }
    return Overview(status.optBoolean("connected", false), snapshots, remotes, canRestore, notes)
}
