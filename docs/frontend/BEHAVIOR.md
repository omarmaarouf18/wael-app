# Frontend & Service Behavior Matrix

Real behavior observed between the Flutter frontend application and the backend services stack (`api-gateway`, `auth-service`, `notification-service`, `redis`, `mongo`) running locally via Docker Compose.

All scenarios were driven through the real application code paths using `ApiClient`, `HttpAuthRepository`, `HttpNotificationRepository`, `AuthProvider`, `NotificationsProvider`, and `NotificationStream`.
All sensitive tokens, OTPs, and secrets are masked (tokens show first 6 characters + `...`; OTPs are 6 digits and are fully masked as `******`).

---

## 1. Summary Matrix

| # | Scenario | Expected Behavior | Observed (App Display + Service Response) | Status |
|---|----------|-------------------|-------------------------------------------|--------|
| 1 | **Signup Happy Path** | Account created, HTTP 201, dev OTP returned | HTTP 201 Created; Account `id=429ec5..., email=matrix_test_...`; dev_otp: `******` | **PASS** |
| 2 | **Duplicate Email** | HTTP 409 Conflict, user-friendly error | HTTP 409 `conflict`: `{"error":"email already registered"}`; App: `"An account with this email already exists."` | **PASS** |
| 3 | **Invalid Signup Input** | HTTP 400 Bad Request, validation feedback | HTTP 400 `invalid_json`: `{"error":"invalid email or password"}`; App: `"Request failed. Please try again."` *(Backend uses generic code)* | **FAIL (Backend)** |
| 4 | **OTP Wrong Code** | HTTP 401 Unauthorized, invalid code error | HTTP 401 `invalid_token`: `{"error":"invalid or expired code"}`; App: `"Invalid or expired verification code."` | **PASS** |
| 5 | **OTP Resend (Unverified)** | Fresh OTP sent, HTTP 200/201 | HTTP 409 `conflict`: `{"error":"email already registered"}`; App: `"An account with this email already exists."` *(Backend missing resend route)* | **FAIL (Backend)** |
| 6 | **OTP Correct Verification** | HTTP 200 OK, JWT pair issued | HTTP 200 OK; access_token: `eyJhbG...`, refresh_token: `33b58f...`; email verified | **PASS** |
| 7 | **OTP Replay / Expired** | HTTP 401 Unauthorized, rejected | HTTP 401 `invalid_token`: `{"error":"invalid or expired code"}`; App: `"Invalid or expired verification code."` | **PASS** |
| 8 | **Login Correct Password** | HTTP 200 OK, JWT pair, profile accessible | HTTP 200 OK; access_token: `eyJhbG...`, refresh_token: `4ec888...`; `/auth/me` returns `email_verified=true` | **PASS** |
| 9 | **Login Wrong Password** | HTTP 401 Unauthorized, sanitized error | HTTP 401 `unauthorized`: `{"error":"invalid credentials"}`; App: `"Invalid credentials. Please verify your details and try again."` | **PASS** |
| 10 | **Lockout (Wrong Passwords)** | 5 failures record backoff; 6th attempt returns HTTP 429 | Attempts 1-5 return HTTP 401; Attempt 6 returns HTTP 429 `locked_out`: `{"error":"too many attempts, retry later"}`; App: `"Too many attempts. Please wait and try again."` | **PASS** |
| 11 | **Password Reset Phase 1** | Request reset OTP, verify code, receive reset token | Request returns dev_otp: `******`; Verify returns reset_token: `0c3691...` | **PASS** |
| 12 | **Password Reset Phase 2** | Confirm new password, old password rejected, new succeeds | Old password login fails HTTP 401; new password login succeeds HTTP 200; JWT pair issued | **PASS** |
| 13 | **Access Token Refresh** | HTTP 200 OK, new access + refresh token | HTTP 200 OK; new access_token: `eyJhbG...`, new refresh_token: `fd681b...` | **PASS** |
| 14 | **Replayed Refresh Token** | HTTP 401 Unauthorized (atomic consume) | HTTP 401 `invalid_token`: `{"error":"invalid refresh token"}`; App logs out | **PASS** |
| 15 | **Logout Token Deadness** | Tokens cleared locally; backend revokes token | App clears local tokens; Backend probe: access token STILL ACCEPTED by `/auth/me` for its full 24-hour lifetime *(stateless JWT)* | **FAIL (Backend)** |
| 16 | **Notification Inbox** | Welcome & password-changed present, mark-read works | 2 notifications retrieved from Mongo via HTTP repository; `markRead(id)` updates `isRead=true` | **PASS** |
| 17 | **Live SSE Delivery** | SSE stream receives real-time frame on event | SSE stream received live event `id=c385fb...`, `title="Password updated"`, `type="security"` on password change | **PASS** |
| 18 | **SSE Reconnect on Restart** | Reconnects after `docker restart` with backoff; no duplicate items | `docker restart wael-notification-service`: Stream backed off, reconnected cleanly, received 2nd frame `37e1ac...`; Provider deduplicated | **PASS** |
| 19 | **Gateway Rate Limit** | Excess requests return HTTP 429 with Retry-After | 100 requests succeed; Requests 101-105 return HTTP 429 `rate_limited`: `{"error":"rate limit exceeded, retry later"}`; App: `"Too many attempts. Please wait and try again."` | **PASS** |
| 20 | **FM-1: Stop API Gateway** | App surfaces network connection error | App caught `SocketException: Connection refused (errno = 111)`; App: `"Unable to reach the server. Please check your connection and try again."` | **PASS** |
| 21 | **FM-2: Stop Auth Service** | Gateway returns 502, app surfaces request failed | Gateway returned HTTP 502 Bad Gateway `{"error":"service unavailable"}`; App: `"Request failed. Please try again."` (Fails closed) | **PASS** |
| 22 | **FM-3: Stop Notification Service** | Gateway returns 502, app shows clean empty/error | Gateway returned HTTP 502; App showed empty/error state (mock fallback restricted to debug); SSE backed off quietly | **PASS** |
| 23 | **FM-4: Stop Redis** | Redis outage handled appropriately | Gateway returns HTTP 429 `rate_limited` (`context deadline exceeded`); App: `"Too many attempts. Please wait and try again."` *(Misleading status for infrastructure outage)* | **FAIL (Backend)** |
| 24 | **FM-5: Stop MongoDB** | Mongo outage handled gracefully | Gateway timed out after 2.0s and returned HTTP 502; auth-service remained healthy but hung waiting for 30s driver selection | **FAIL (Backend)** |

---

## 2. Detailed Scenario Execution & Raw Captured Outputs

### 2.1 Signup Happy Path & Validation
```
=== [SCENARIO 1] signup happy path ===
Observed account: id=429ec517-e2e5-41f4-9301-b21a91be60f8, email=matrix_test_1790750241498@example.com, role=user
Observed dev_otp: ******
Result: PASS (HTTP 201 Created)

=== [SCENARIO 2] duplicate email ===
Observed service response: status=409, code=conflict, error=email already registered
Observed app display: An account with this email already exists.
Result: PASS

=== [SCENARIO 3] invalid input ===
Observed service response: status=400, code=invalid_json, error=invalid email or password
Observed app display: Request failed. Please try again.
Result: FAIL (Backend code invalid_json obscures validation errors)
```

### 2.2 OTP Verification & Replay Protection
```
=== [SCENARIO 4] OTP wrong ===
Observed service response: status=401, code=invalid_token, error=invalid or expired code
Observed app display: Invalid or expired verification code.
Result: PASS

=== [SCENARIO 6] OTP correct ===
Observed access_token: eyJhbG...
Observed refresh_token: 33b58f...
Result: PASS (Tokens issued, email_verified marked true in database)

=== [SCENARIO 7] OTP expired / replayed ===
Observed service response: status=401, code=invalid_token, error=invalid or expired code
Result: PASS (Code atomically consumed upon first verification)
```

### 2.3 Authentication, Lockout, and Token Rotation
```
=== [SCENARIO 8] login correct ===
Observed access_token: eyJhbG...
Observed refresh_token: 4ec888...
Observed me: email=matrix_test_1790750241498@example.com, role=user, email_verified=true
Result: PASS

=== [SCENARIO 9] login wrong password ===
Observed service response: status=401, code=unauthorized, error=invalid credentials
Observed app display: Invalid credentials. Please verify your details and try again.
Result: PASS

=== [SCENARIO 10] lockout and 429 message ===
Attempt 1: status=401, code=unauthorized, message=invalid credentials
Attempt 2: status=401, code=unauthorized, message=invalid credentials
Attempt 3: status=401, code=unauthorized, message=invalid credentials
Attempt 4: status=401, code=unauthorized, message=invalid credentials
Attempt 5: status=401, code=unauthorized, message=invalid credentials
Attempt 6: status=429, code=locked_out, message=too many attempts, retry later
Observed app display on lockout: Too many attempts. Please wait and try again.
Result: PASS

=== [SCENARIO 13 & 14] token refresh and replayed refresh token ===
Rotated access_token: eyJhbG...
Rotated refresh_token: fd681b...
Observed replayed refresh token rejection: status=401, code=invalid_token, error=invalid refresh token
Result: PASS (Atomic take prevents token replay)

=== [SCENARIO 15] logout token deadness & remaining validity re-test ===
Local App State: Tokens erased from flutter_secure_storage.
Decoded Access Token Claims (shared/infra/jwtutil/jwt.go):
{
  "user_id": "b1121d6c-09d4-45e8-878d-12d9c63e8f2a",
  "role": "user",
  "email": "jwt_validity_1790750978@example.com",
  "exp": 1790837378,
  "nbf": 1790750978,
  "iat": 1790750978,
  "jti": "83a58a40-78c9-49c0-b270-5c6fc9e14abb"
}
Total validity (exp - iat): 86400 seconds (24.0 hours)
Remaining validity at probe: 86400 seconds (24.00 hours)
Probe GET /api/v1/auth/me with Bearer token:
HTTP/1.1 200 OK
{"email":"jwt_validity_1790750978@example.com","email_verified":true,"id":"b1121d6c-09d4-45e8-878d-12d9c63e8f2a","role":"user"}
Result: FAIL (Backend lacks /auth/logout and server-side revocation list; 24-hour token survives local logout)
```

### 2.4 Notifications Inbox and Live SSE Stream
```
=== [SCENARIO 16] notifications inbox ===
Observed notification inbox items: 2
  - id=7c09152b-5669-49ef-9686-0fc89bc3ef7e, type=security, title="Password updated", isRead=false
  - id=93d791d1-b19a-4f8b-903b-b6117acf3f42, type=system, title="Welcome", isRead=false
Welcome notification present: true
Password changed notification present: true
Marking notification read: id=7c09152b-5669-49ef-9686-0fc89bc3ef7e
Notification read status after markRead: true
Result: PASS

=== [SCENARIO 17] SSE live frame delivery ===
Observed live SSE frame: {
  id: c385fb91-9db6-4254-9388-e1092803dc85,
  user_id: 429ec517-e2e5-41f4-9301-b21a91be60f8,
  title: Password updated,
  title_ar: تم تحديث كلمة المرور,
  body: Your password was just changed. Contact support if this was not you.,
  body_ar: تم تغيير كلمة المرور للتو. تواصل مع الدعم إذا لم تكن أنت.,
  type: security,
  target_route: /settings,
  read: false,
  created_at: 2026-09-30T06:37:22.875756522Z
}
Result: PASS

=== [SCENARIO 18] SSE reconnect after docker restart ===
Live SSE frame 1 received: id=2ff80cfb-88e5-4aa8-be96-928761a74dd1, title="Password updated"
Restarting notification-service via docker restart:
  docker restart exitCode=0, stdout=wael-notification-service
Waiting for notification-service to restart and SSE client to backoff & reconnect...
Live SSE frame 2 received: id=37e1ac3b-62d7-4e32-a971-8ac8edf97dbe, title="Password updated"
Total items received by listener: 2
Total items in NotificationsProvider: 2
After re-adding first item, NotificationsProvider count: 2 (Deduplication confirmed)
Result: PASS
```

### 2.5 Gateway Rate Limiting
```
Starting rapid requests to trigger Gateway rate limit (limit=100/min)...
Request 101 hit 429: status=429, code=rate_limited, message="rate limit exceeded, retry later"
Request 102 hit 429: status=429, code=rate_limited, message="rate limit exceeded, retry later"
Request 103 hit 429: status=429, code=rate_limited, message="rate limit exceeded, retry later"
Request 104 hit 429: status=429, code=rate_limited, message="rate limit exceeded, retry later"
Request 105 hit 429: status=429, code=rate_limited, message="rate limit exceeded, retry later"
Finished burst: non-429 count=100, 429 count=5
Observed app display on gateway rate limit: "Too many attempts. Please wait and try again."
Result: PASS
```

### 2.6 Failure Modes (Container Fault Injection)
```
=== [FAILURE MODE 1] Stop API Gateway ===
Command: docker stop wael-api-gateway
Observed raw client exception: ClientException with SocketException: Connection refused (OS Error: Connection refused, errno = 111), address = localhost, port = 51200, uri=https://localhost:18080/api/v1/auth/signup
Observed app display: "Unable to reach the server. Please check your connection and try again."
Service behavior: Gateway process terminated, TCP connection refused.
Result: PASS

=== [FAILURE MODE 2] Stop Auth Service ===
Command: docker stop wael-auth-service
Observed gateway response: status=502, code=null, message="service unavailable"
Observed app display: "Request failed. Please try again."
Service behavior: Gateway fails closed (502 Bad Gateway), rejecting requests.
Result: PASS

=== [FAILURE MODE 3] Stop Notification Service ===
Command: docker stop wael-notification-service
Observed notification call response: status=502, code=null, message="service unavailable"
Observed app display: "Request failed. Please try again."
Service behavior: Gateway returns 502; App displays empty/error state without mock injection; SSE client backs off quietly.
Result: PASS

=== [FAILURE MODE 4] Stop Redis ===
Command: docker stop wael-redis
Client request: POST https://localhost:18080/api/v1/auth/signup
Raw HTTP Response:
HTTP/2 429
content-type: application/json
retry-after: 30ns
{"code":"rate_limited","error":"rate limit exceeded, retry later"}
Observed Gateway log:
[SECURITY CRITICAL] Redis rate limiter error (FAIL CLOSED): context deadline exceeded. Restricting traffic for key: gw:172.21.0.1
[GATEWAY] POST /api/v1/auth/signup 429 2.000924044s
Observed Auth Service log:
No request reached auth-service (request blocked at gateway edge).
Observed App Display: "Too many attempts. Please wait and try again."
Component producing response: api-gateway (RateLimit middleware).
Result: FAIL (Backend rate limiter intercepts Redis dependency failure and reports misleading client-side rate limit)

=== [FAILURE MODE 5] Stop MongoDB ===
Command: docker stop wael-mongo
Container status during test window:
NAME                        STATUS
wael-api-gateway            Up 16 minutes (healthy)
wael-auth-service           Up 16 minutes (healthy)
wael-notification-service   Up 18 minutes (healthy)
wael-redis                  Up 18 minutes (healthy)
wael-mongo                  Exited (0)
Client request: POST https://localhost:18080/api/v1/auth/signup
Raw HTTP Response:
HTTP/2 502
content-type: text/plain; charset=utf-8
{"error": "service unavailable", "target": "/api/v1/auth/"}
Observed Gateway log:
[PROXY ERROR] POST /auth/signup -> https://auth-service:3002: context canceled
[GATEWAY] POST /api/v1/auth/signup 502 2.001392381s
Observed Auth Service log:
[ERROR] a288a13bf8fd3e49 conflict POST /auth/signup: store: insert: server selection error: context canceled, current topology: { Type: Single, Servers: [{ Addr: mongo:27017, Type: Unknown, Last error: dial tcp: lookup mongo on 127.0.0.11:53: no such host }, ] }
Component producing response: api-gateway (proxy reverse-proxy ErrorHandler).
Result: FAIL (auth-service hung waiting for default 30s Mongo server selection timeout without request deadline; gateway timed out at 2.0s; auth-service handled DB error as conflict)
```

---

## 3. Evidence for In-Memory Fallbacks Audit

Under test scenario `FM-4 (Stop Redis)`, we examined how the stack handles Redis dependency failure:
1. **API Gateway Rate Limiter**: When Redis is stopped, `RateLimiter.CheckAndRecord` catches the context deadline error, logs `[SECURITY CRITICAL] Redis rate limiter error (FAIL CLOSED)...`, and returns `(true, 30s)`. The gateway terminates the request and responds with HTTP 429 and `Retry-After: 30ns`. Traffic is blocked rather than allowed through unthrottled.
2. **Auth Service Lockout**: In `shared/infra/ratelimit/ratelimit.go`, `AuthRateLimiter.IsLocked` logs `[SECURITY CRITICAL] Redis IsLocked check error (FAIL CLOSED)...` and returns `(true, 5m)`.
3. **JWT Denylist**: In `shared/infra/jwtutil/jwt.go`, `ValidateToken` logs `[SECURITY CRITICAL] Redis error checking JWT denylist (FAIL CLOSED)...` and rejects any presented token with an error, returning HTTP 401 Unauthorized.
4. **Assessment**: The system strictly **fails closed** rather than falling back to in-memory bypasses. However, as noted in Finding 3, reporting a Redis outage as an HTTP 429 client rate limit is misleading to users.

---

## 4. Backend Findings

The following backend issues were observed during live matrix verification. The backend codebase is read-only for this agent, so these issues are documented here for the backend owner.

### Finding 1: Missing OTP Resend Endpoint
- **Severity**: High (Blocks account verification)
- **Description**: `auth-service` has no `/auth/resend-otp` route. If an unverified user requests another OTP by calling `POST /api/v1/auth/signup` with their email and password, the handler checks `Store.FindByEmail` and unconditionally returns HTTP 409 Conflict. If the user's initial OTP code expires (10 minutes) or is lost, the user cannot re-trigger an OTP code and cannot log in (`403 email not verified`).
- **Reproduction Steps**:
  1. Send `POST /api/v1/auth/signup` with `{"email": "unverified@example.com", "password": "Password123!"}`. Returns HTTP 201 with OTP.
  2. Without verifying the OTP, send the same `POST /api/v1/auth/signup` request again (simulating an app "Resend Code" action).
- **Raw Service Response**:
  ```http
  HTTP/1.1 409 Conflict
  Content-Type: application/json
  {"error":"email already registered","code":"conflict"}
  ```
- **Recommended Fix**: Add a dedicated `POST /auth/resend-otp` endpoint, or if `FindByEmail` finds an unverified user (`!u.EmailVerified`), regenerate a fresh OTP, update `s.Codes.Set`, send the email, and return HTTP 200/201.

---

### Finding 2: Stateless Logout Without Server-Side Token Revocation (24-Hour Token Lifetime)
- **Severity**: High (Security / Session Invalidation)
- **Description**: Access tokens generated by `shared/infra/jwtutil/jwt.go` have a **24-hour expiration** (`time.Now().Add(24 * time.Hour)`), not 15 minutes. When a user logs out in the Flutter application, `AuthProvider.logout()` deletes local tokens from `flutter_secure_storage`. However, `auth-service` exposes no `POST /auth/logout` endpoint, and neither the gateway nor the auth service revokes the access token in Redis. Even though `shared/infra/jwtutil.RevokeToken(token)` exists in the codebase, it is never called. As verified in Scenario 15, the token remains fully valid against `/api/v1/auth/me` and downstream microservices for its entire remaining 24 hours.
- **Reproduction Steps**:
  1. Log in via `POST /api/v1/auth/login` and capture `access_token`. Decode payload: `exp - iat = 86400s (24 hours)`.
  2. Perform user logout in Flutter (`AuthProvider.logout()`). Local store is cleared.
  3. Send `GET /api/v1/auth/me` with header `Authorization: Bearer <captured_access_token>`.
- **Raw Service Response**:
  ```http
  HTTP/1.1 200 OK
  Content-Type: application/json
  {"id":"b1121d6c-09d4-45e8-878d-12d9c63e8f2a","email":"jwt_validity_1790750978@example.com","role":"user","email_verified":true}
  ```
- **Recommended Fix**: Expose `POST /auth/logout` in `auth-service` that calls `jwtutil.RevokeToken(token)` to store the token's `jti` in Redis until expiry. Reduce access token lifetime to 15 minutes if paired with refresh token rotation.

---

### Finding 3: Redis Infrastructure Outage Reported as Client Rate Limit (HTTP 429) & Buggy Retry-After Header
- **Severity**: Medium (Misleading Error & Protocol Bug)
- **Description**: When Redis is down, `api-gateway`'s `RateLimit` middleware fails closed because Redis ping/eval times out (`context deadline exceeded`). The gateway returns HTTP 429 `{"code":"rate_limited","error":"rate limit exceeded, retry later"}`. To the user and client app, an internal infrastructure failure is reported as "Too many requests. Please wait and try again." Furthermore, in `services/api-gateway/internal/middleware/limiter.go` line 47, the header is formatted as `time.Duration(retryAfter.Seconds()).String()`, which converts the integer 30 to nanoseconds, emitting the malformed header `retry-after: 30ns`.
- **Reproduction Steps**:
  1. Stop Redis: `docker stop wael-redis`.
  2. Send `POST /api/v1/auth/signup` to the gateway.
- **Raw Service Response**:
  ```http
  HTTP/2 429 
  content-type: application/json
  retry-after: 30ns
  {"code":"rate_limited","error":"rate limit exceeded, retry later"}
  ```
- **Recommended Fix**: When `limiter.CheckAndRecord` encounters a Redis connectivity error, the gateway should return HTTP 503 Service Unavailable or 500 Internal Server Error rather than HTTP 429. Format `Retry-After` as integer seconds (e.g. `strconv.Itoa(int(retryAfter.Seconds()))`).

---

### Finding 4: Client IP Resolution and Lockout Scope Behind Real Proxies
- **Severity**: Medium (Availability / False Lockout Risk)
- **Description**: 
  - `auth-service` tracks failed logins using both `login:email:<email>` and `login:ip:<ip>`, with backoff engaging on the 6th failed attempt.
  - Client IP resolution in `api-gateway` (`services/api-gateway/internal/proxy/proxy.go`) checks `iputil.IsTrustedProxy(immediateIP, trustedProxies)`.
  - **Single IP Bucket Under Reverse Proxy**: If `api-gateway` is deployed behind a real reverse proxy (e.g. Caddy, Nginx, or cloud load balancer) whose IP or CIDR is **not** included in `TRUSTED_PROXY_IPS`, the gateway rewrites `X-Forwarded-For` with `req.RemoteAddr` (the proxy's IP). `auth-service`'s `handlerutil.GetIP(r)` will then extract the proxy's IP for every request. As a result, **all users connecting through that proxy share a single IP lockout bucket**; if one user fails 5 attempts, all users behind that proxy are locked out for 30s.
  - **Shared NAT / Campus WiFi Scope**: Even with `TRUSTED_PROXY_IPS` correctly configured, users sharing a single corporate NAT, university WiFi, or mobile carrier CGNAT share a common egress IP in `parts[0]`. A single user triggering lockout blocks all other users on that network.
- **Recommended Fix**: Document `TRUSTED_PROXY_IPS` configuration requirements for production edge proxies. Consider locking by `email` only for backoff, or using IP-based rate limiting with higher thresholds while reserving short lockouts strictly for `email`.

---

### Finding 5: Validation Errors Return `invalid_json` Error Code
- **Severity**: Low (UX / Error Clarity)
- **Description**: In `services/auth-service/internal/handlers/auth.go`, when signup inputs fail business validation (such as malformed email syntax or password length < 8 characters), the handler returns `handlerutil.WriteSafeError(w, r, http.StatusBadRequest, handlerutil.ErrCodeInvalidJSON, "invalid email or password", nil)`. The error code is `invalid_json`.
- **Impact**: The app cannot distinguish malformed JSON syntax errors from field validation errors. The app's `ErrorMessages.forApiError` falls back to `"Request failed. Please try again."` instead of displaying field-specific validation feedback to the user.
- **Recommended Fix**: Return specific error codes like `validation_failed` or `invalid_field` with field details.

---

### Finding 6: Mongo Store Error Handling in Signup Masks Database Outages as Conflict
- **Severity**: Medium (Error Masking & Timeout)
- **Description**: In `services/auth-service/internal/handlers/auth.go` line 136:
  ```go
  if err := s.Store.Create(ctx, u); err != nil {
      handlerutil.WriteSafeError(w, r, http.StatusConflict, handlerutil.ErrCodeConflict, "email already registered", err)
      return
  }
  ```
  If `s.Store.Create` fails due to a database connection error or driver failure, `auth-service` logs and writes `409 Conflict: "email already registered"` instead of `500 Internal Server Error`.
  Furthermore, when MongoDB is down, the Mongo driver's default server selection timeout is 30 seconds. Because `auth-service` sets no timeout on `ctx`, the request hangs until `api-gateway`'s 2.0-second timeout cancels the request, producing a gateway 502.
- **Recommended Fix**: Differentiate `mongo.IsDuplicateKeyError(err)` from connection/topology errors in `auth.go`. Add explicit timeouts (e.g. 3-5 seconds) to database queries in `auth-service`.

---

## 5. Items Not Verified / Out of Scope

1. **Academy & Course Endpoints**:
   - The academy service is currently being implemented by the parallel agent. Academy content in the Flutter app remains mock-backed behind repository interfaces (`MockAcademyRepository`) as instructed. *(Superseded 2026-10-01: home, courses and course detail now read the academy service through `AcademyRepository`; `MockAcademyRepository` is deleted. Ebooks and payment are still bundled mock data.)*
2. **Third-party Email Delivery (Resend API)**:
   - Email dispatch was tested using dev OTPs (`s.devOTPField()` returning `dev_otp` in JSON response under `local` environment) rather than actual external SMTP/Resend delivery.
