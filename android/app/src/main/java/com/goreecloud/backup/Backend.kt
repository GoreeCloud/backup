package com.goreecloud.backup

import android.content.ContentResolver
import android.util.Base64
import android.net.Uri
import android.provider.OpenableColumns

import org.json.JSONArray
import org.json.JSONObject
import java.io.ByteArrayOutputStream
import java.net.HttpURLConnection
import java.security.MessageDigest
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
    val mobileUploadEnabled: Boolean,
    val readOnly: Boolean,
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
    val info = try {
        JSONObject(apiJson(server, "/api/v1/info", token))
    } catch (_: Exception) {
        notes += "Restore and mobile ingest capabilities could not be checked."
        null
    }
    return Overview(status.optBoolean("connected", false), snapshots, remotes,
        info?.optBoolean("restoreEnabled", false) ?: false,
        info?.optBoolean("mobileUploadEnabled", false) ?: false,
        info?.optBoolean("readOnly", true) ?: true,
        notes)
}

/** Streams one SAF-selected file to the authenticated opt-in ingest endpoint.
 * No file bytes or content URI are stored in app storage or sent as URL params.
 */
internal fun uploadSelectedDocument(
    resolver: ContentResolver, uri: Uri, server: URL, token: String
): JSONObject {
    require(server.protocol == "https") { "Trusted HTTPS is required." }
    require(token.length in 32..1024 && token.none { it.isWhitespace() }) { "Invalid token." }
    val fileName = resolver.query(uri, arrayOf(OpenableColumns.DISPLAY_NAME), null, null, null)?.use { cursor ->
        if (cursor.moveToFirst()) cursor.getString(0) else null
    } ?: throw IllegalArgumentException("Selected document has no display name.")
    require(fileName.length in 1..160 && fileName.none { it.isISOControl() || it == '/' || it == '\\' } && fileName != "." && fileName != "..") {
        "Selected document name is not supported."
    }
    val digest = MessageDigest.getInstance("SHA-256")
    var sentBytes = 0L
    val connection = URL(server.toString().trimEnd('/') + "/api/v1/mobile/files").openConnection() as HttpURLConnection
    try {
        connection.instanceFollowRedirects = false
        connection.requestMethod = "POST"
        connection.setRequestProperty("Authorization", "Bearer $token")
        connection.setRequestProperty("Content-Type", "application/octet-stream")
        connection.setRequestProperty("X-GCB-Filename-B64", Base64.encodeToString(
            fileName.toByteArray(Charsets.UTF_8), Base64.NO_WRAP or Base64.URL_SAFE or Base64.NO_PADDING
        ))
        connection.setRequestProperty("X-GCB-Confirm-Upload", "true")
        connection.setRequestProperty("Accept", "application/json")
        connection.connectTimeout = 8_000
        connection.readTimeout = 30 * 60 * 1000
        connection.useCaches = false
        connection.doOutput = true
        connection.setChunkedStreamingMode(8192)
        resolver.openInputStream(uri)?.use { source ->
            connection.outputStream.use { destination ->
                val bytes = ByteArray(8192)
                var total = 0L
                while (true) {
                    val read = source.read(bytes)
                    if (read < 0) break
                    total += read
                    require(total <= 32L * 1024L * 1024L) { "Selected file exceeds the 32 MiB limit." }
                    destination.write(bytes, 0, read)
                    digest.update(bytes, 0, read)
                }
                require(total > 0) { "The selected document is empty." }
                sentBytes = total
            }
        } ?: throw IllegalArgumentException("Selected document cannot be opened.")
        val code = connection.responseCode
        if (code != 201) throw IllegalStateException("Selected-file backup failed (HTTP $code).")
        val result = JSONObject(boundedResponse(connection))
        require(result.optBoolean("backedUp", false)) { "Backup completion was not confirmed." }
        require(result.optLong("bytes", -1) == sentBytes) { "Server file byte count did not match the selected document." }
        require(result.optString("sha256") == hexSha256(digest.digest())) {
            "Server checksum did not match the selected document."
        }
        return result
    } finally {
        connection.disconnect()
    }
}


internal fun hexSha256(bytes: ByteArray): String {
    val symbols = "0123456789abcdef"
    return buildString(bytes.size * 2) {
        for (value in bytes) {
            val unsigned = value.toInt() and 0xff
            append(symbols[unsigned ushr 4])
            append(symbols[unsigned and 15])
        }
    }
}
