package com.goreecloud.backup

import org.junit.Assert.assertEquals
import org.junit.Assert.fail
import org.junit.Test

class EndpointValidationTest {
    @Test
    fun acceptsBareTrustedHttpsOrigin() {
        assertEquals("https://backup.example.com", checkedHttpsEndpoint("https://backup.example.com").toString())
        assertEquals("https://backup.example.com", checkedHttpsEndpoint("  https://backup.example.com/  ").toString())
        assertEquals("https://backup.example.com:8443", checkedHttpsEndpoint("https://backup.example.com:8443").toString())
    }

    @Test
    fun blocksUntrustedOrCredentialBearingOrigins() {
        val invalid = listOf(
            "",
            "http://backup.example.com",
            "ftp://backup.example.com",
            "https://",
            "https://user:pass@backup.example.com",
            "https://backup.example.com@malicious.example",
            "https://backup.example.com/other",
            "https://backup.example.com?token=hidden",
            "https://backup.example.com#fragment",
            "https://backup.example.com:0",
            "https://backup.example.com:65536",
            "https://backup.example.com/../other"
        )
        for (candidate in invalid) {
            try {
                checkedHttpsEndpoint(candidate)
                fail("Accepted invalid endpoint: $candidate")
            } catch (_: IllegalArgumentException) {
                // Expected: keep credentials off untrusted endpoints.
            }
        }
    }
}
