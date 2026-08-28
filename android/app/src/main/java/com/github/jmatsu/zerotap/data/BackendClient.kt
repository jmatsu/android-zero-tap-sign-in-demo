package com.github.jmatsu.zerotap.data

import java.io.IOException
import kotlin.coroutines.resumeWithException
import kotlinx.coroutines.suspendCancellableCoroutine
import kotlinx.serialization.encodeToString
import kotlinx.serialization.json.Json
import kotlinx.serialization.json.JsonObject
import okhttp3.Call
import okhttp3.Callback
import okhttp3.MediaType.Companion.toMediaType
import okhttp3.OkHttpClient
import okhttp3.Request
import okhttp3.RequestBody.Companion.toRequestBody
import okhttp3.Response
import kotlin.coroutines.resume

/** Raised when the backend answers with a non-2xx status. */
class BackendException(val status: Int, message: String) : IOException(message)

/**
 * Thin HTTP client for the demo backend, and a plain OkHttp one on purpose:
 * nothing about Zero-Tap Sign-In needs a particular networking stack.
 *
 * The one decision worth carrying over is that every WebAuthn payload travels as
 * raw JSON, so neither side models the WebAuthn structures a second time. The
 * restore endpoints are the four `/api/restore/...` calls below; the rest is
 * ordinary session plumbing.
 */
class BackendClient(
    private val baseUrl: String,
    private val json: Json,
    private val client: OkHttpClient = OkHttpClient(),
) {
    suspend fun signUp(username: String, password: String): AuthResponse =
        post("/api/signup", token = null, body = json.encodeToString(PasswordRequest(username, password)))

    suspend fun signInWithPassword(username: String, password: String): AuthResponse =
        post("/api/login/password", token = null, body = json.encodeToString(PasswordRequest(username, password)))

    suspend fun me(token: String): UserView = post("/api/me", token = token, body = null, method = "GET")

    suspend fun signOut(token: String) {
        postRaw("/api/logout", token, null)
    }

    suspend fun beginPasskeyRegistration(token: String): BeginResponse =
        post("/api/passkey/register/begin", token, null)

    suspend fun finishPasskeyRegistration(token: String, ceremonyId: String, credentialJson: String): RegisterResponse =
        post("/api/passkey/register/finish", token, finishBody(ceremonyId, credentialJson))

    suspend fun beginPasskeySignIn(): BeginResponse = post("/api/passkey/login/begin", token = null, body = null)

    suspend fun finishPasskeySignIn(ceremonyId: String, credentialJson: String): AuthResponse =
        post("/api/passkey/login/finish", token = null, body = finishBody(ceremonyId, credentialJson))

    suspend fun beginRestoreRegistration(token: String): BeginResponse =
        post("/api/restore/register/begin", token, null)

    suspend fun finishRestoreRegistration(token: String, ceremonyId: String, credentialJson: String): RegisterResponse =
        post("/api/restore/register/finish", token, finishBody(ceremonyId, credentialJson))

    suspend fun beginRestoreSignIn(): BeginResponse = post("/api/restore/login/begin", token = null, body = null)

    suspend fun finishRestoreSignIn(ceremonyId: String, credentialJson: String): AuthResponse =
        post("/api/restore/login/finish", token = null, body = finishBody(ceremonyId, credentialJson))

    suspend fun revokeRestoreKeys(token: String) {
        postRaw("/api/restore/revoke", token, null)
    }

    private fun finishBody(ceremonyId: String, credentialJson: String): String =
        json.encodeToString(FinishRequest(ceremonyId, json.parseToJsonElement(credentialJson)))

    private suspend inline fun <reified T> post(
        path: String,
        token: String?,
        body: String?,
        method: String = "POST",
    ): T = json.decodeFromString(postRaw(path, token, body, method))

    private suspend fun postRaw(path: String, token: String?, body: String?, method: String = "POST"): String {
        val request = Request.Builder()
            .url(baseUrl + path)
            .method(method, if (method == "GET") null else (body ?: "").toRequestBody(JSON_MEDIA_TYPE))
            .apply { token?.let { header("Authorization", "Bearer $it") } }
            .build()

        return client.newCall(request).await()
    }

    private fun errorMessage(payload: String, status: Int): String = runCatching {
        json.decodeFromString(ErrorResponse.serializer(), payload).error
    }.getOrElse { "HTTP $status" }

    /**
     * Suspends until the response body has been read.
     *
     * Reading the body is a blocking socket call, so it happens here on
     * OkHttp's dispatcher thread rather than on whatever thread resumes the
     * coroutine — every caller in this app is on the main thread.
     */
    private suspend fun Call.await(): String = suspendCancellableCoroutine { continuation ->
        enqueue(object : Callback {
            override fun onFailure(call: Call, e: IOException) = continuation.resumeWithException(e)

            override fun onResponse(call: Call, response: Response) {
                val result = runCatching {
                    response.use {
                        val payload = it.body.string()
                        if (!it.isSuccessful) {
                            throw BackendException(it.code, errorMessage(payload, it.code))
                        }
                        payload
                    }
                }
                continuation.resumeWith(result)
            }
        })
        continuation.invokeOnCancellation { runCatching { cancel() } }
    }

    private companion object {
        val JSON_MEDIA_TYPE = "application/json; charset=utf-8".toMediaType()
    }
}
