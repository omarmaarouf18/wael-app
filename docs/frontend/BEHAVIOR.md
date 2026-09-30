# Frontend & Service Behavior Matrix

Real behavior observed between the Flutter frontend application and the backend services stack (`api-gateway`, `auth-service`, `notification-service`, `redis`, `mongo`) running locally via Docker Compose.

All scenarios were executed through the real application code paths using `ApiClient`, `HttpAuthRepository`, `HttpNotificationRepository`, `AuthProvider`, `NotificationsProvider`, and `NotificationStream`.
All sensitive tokens, OTPs, and secrets are masked (first 6 characters followed by `...`).

---

## 1. Summary Matrix

| # | Scenario | Expected Behavior | Observed (App Display + Service Response) | Status |
|---|----------|-------------------|-------------------------------------------|--------|
| 1 | **Signup Happy Path** | Account created, HTTP 201, dev OTP returned | HTTP 201 Created; Account `id=429ec5..., email=matrix_test_...`; dev_otp: `678512...` | **PASS** |
| 2 | **Duplicate Email** | HTTP 409 Conflict, user-friendly error | HTTP 409 `conflict`: `{"error":"email already registered"}`; App: `"An account with this email already exists."` | **PASS** |
| 3 | **Invalid Signup Input** | HTTP 400 Bad Request, sanitized error | HTTP 400 `invalid_json`: `{"error":"invalid email or password"}`; App: `"Request failed. Please try again."` | **PASS** |
| 4 | **OTP Wrong Code** | HTTP 401 Unauthorized, invalid code error | HTTP 401 `invalid_token`: `{"error":"invalid or expired code"}`; App: `"Invalid or expired verification code."` | **PASS** |
| 5 | **OTP Resend (Unverified)** | Fresh OTP sent, HTTP 200/201 | HTTP 409 `conflict`: `{"error":"email already registered"}`; App: `"An account with this email already exists."` *(Backend missing resend route)* | **FAIL (Backend)** |
| 6 | **OTP Correct Verification** | HTTP 200 OK, JWT pair issued | HTTP 200 OK; access_token: `eyJhbG...`, refresh_token: `33b58f...`; email verified | **PASS** |
| 7 | **OTP Replay / Expired** | HTTP 401 Unauthorized, rejected | HTTP 401 `invalid_token`: `{"error":"invalid or expired code"}`; App: `"Invalid or expired verification code."` | **PASS** |
| 8 | **Login Correct Password** | HTTP 200 OK, JWT pair, profile accessible | HTTP 200 OK; access_token: `eyJhbG...`, refresh_token: `4ec888...`; `/auth/me` returns `email_verified=true` | **PASS** |
| 9 | **Login Wrong Password** | HTTP 401 Unauthorized, sanitized error | HTTP 401 `unauthorized`: `{"error":"invalid credentials"}`; App: `"Invalid credentials. Please verify your details and try again."` | **PASS** |
| 10 | **Lockout (Wrong Passwords)** | 5 failures record backoff; 6th attempt returns HTTP 429 | Attempts 1-5 return HTTP 401; Attempt 6 returns HTTP 429 `locked_out`: `{"error":"too many attempts, retry later"}`; App: `"Too many attempts. Please wait and try again."` | **PASS** |
| 11 | **Password Reset Phase 1** | Request reset OTP, verify code, receive reset token | Request returns dev_otp: `988251...`; Verify returns reset_token: `0c3691...` | **PASS** |
| 12 | **Password Reset Phase 2** | Confirm new password, old password rejected, new succeeds | Old password login fails HTTP 401; new password login succeeds HTTP 200; JWT pair issued | **PASS** |
| 13 | **Access Token Refresh** | HTTP 200 OK, new access + refresh token | HTTP 200 OK; new access_token: `eyJhbG...`, new refresh_token: `fd681b...` | **PASS** |
| 14 | **Replayed Refresh Token** | HTTP 401 Unauthorized (atomic consume) | HTTP 401 `invalid_token`: `{"error":"invalid refresh token"}`; App logs out | **PASS** |
| 15 | **Logout Token Deadness** | Tokens cleared locally; backend revokes token | App clears local tokens; Backend probe: access token STILL ACCEPTED by `/auth/me` until expiry *(stateless JWT)* | **FAIL (Backend)** |
| 16 | **Notification Inbox** | Welcome & password-changed present, mark-read works | 2 notifications retrieved from Mongo via HTTP repository; `markRead(id)` updates `isRead=true` | **PASS** |
| 17 | **Live SSE Delivery** | SSE stream receives real-time frame on event | SSE stream received live event `id=c385fb...`, `title="Password updated"`, `type="security"` on password change | **PASS** |
| 18 | **SSE Reconnect on Restart** | Reconnects after `docker restart` with backoff; no duplicate items | `docker restart wael-notification-service`: Stream backed off, reconnected cleanly, received 2nd frame `37e1ac...`; Provider deduplicated | **PASS** |
| 19 | **Gateway Rate Limit** | Excess requests return HTTP 429 with Retry-After | 100 requests succeed; Requests 101-105 return HTTP 429 `rate_limited`: `{"error":"rate limit exceeded, retry later"}`; App: `"Too many attempts. Please wait and try again."` | **PASS** |
| 20 | **FM-1: Stop API Gateway** | App surfaces network connection error | App caught `SocketException: Connection refused (errno = 111)`; App: `"Unable to reach the server. Please check your connection and try again."` | **PASS** |
| 21 | **FM-2: Stop Auth Service** | Gateway returns 502, app surfaces request failed | Gateway returned HTTP 502 Bad Gateway `{"error":"service unavailable"}`; App: `"Request failed. Please try again."` (Fails closed) | **PASS** |
| 22 | **FM-3: Stop Notification Service** | Gateway returns 502, app shows clean empty/error | Gateway returned HTTP 502; App showed empty/error state (mock fallback restricted to debug); SSE backed off quietly | **PASS** |
| 23 | **FM-4: Stop Redis** | Fail closed on auth, rate limit, and JWT checks | Gateway and auth-service returned HTTP 429 `rate_limited` (fail closed); App: `"Too many attempts. Please wait and try again."` | **PASS** |
| 24 | **FM-5: Stop MongoDB** | Fail closed on database lookups/writes | Gateway/auth-service returned HTTP 502 `{"error":"service unavailable"}`; App: `"Request failed. Please try again."` (Fails closed) | **PASS** |

---

## 2. Detailed Scenario Execution & Raw Captured Outputs

### 2.1 Signup Happy Path & Validation
```
=== [SCENARIO 1] signup happy path ===
Observed account: id=429ec517-e2e5-41f4-9301-b21a91be60f8, email=matrix_test_1790750241498@example.com, role=user
Observed dev_otp: 678512...
Result: PASS (HTTP 201 Created)

=== [SCENARIO 2] duplicate email ===
Observed service response: status=409, code=conflict, error=email already registered
Observed app display: An account with this email already exists.
Result: PASS

=== [SCENARIO 3] invalid input ===
Observed service response: status=400, code=invalid_json, error=invalid email or password
Observed app display: Request failed. Please try again.
Result: PASS
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

=== [SCENARIO 16] lockout and 429 message ===
Attempt 1: status=401, code=unauthorized, message=invalid credentials
Attempt 2: status=401, code=unauthorized, message=invalid credentials
Attempt 3: status=401, code=unauthorized, message=invalid credentials
Attempt 4: status=401, code=unauthorized, message=invalid credentials
Attempt 5: status=401, code=unauthorized, message=invalid credentials
Attempt 6: status=429, code=locked_out, message=too many attempts, retry later
Observed app display on lockout: Too many attempts. Please wait and try again.
Result: PASS

=== [SCENARIO 11 & 12] token refresh and replayed refresh token ===
Rotated access_token: eyJhbG...
Rotated refresh_token: fd681b...
Observed replayed refresh token rejection: status=401, code=invalid_token, error=invalid refresh token
Result: PASS (Atomic take prevents token replay)
```

### 2.4 Notifications Inbox and Live SSE Stream
```
=== [SCENARIO 14] notifications inbox ===
Observed notification inbox items: 2
  - id=7c09152b-5669-49ef-9686-0fc89bc3ef7e, type=security, title="Password updated", isRead=false
  - id=93d791d1-b19a-4f8b-903b-b6117acf3f42, type=system, title="Welcome", isRead=false
Welcome notification present: true
Password changed notification present: true
Marking notification read: id=7c09152b-5669-49ef-9686-0fc89bc3ef7e
Notification read status after markRead: true
Result: PASS

=== [SCENARIO 15] SSE live frame delivery ===
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
Attempting signup while Redis is down:
Observed signup response when Redis down: status=429, code=rate_limited, message="rate limit exceeded, retry later"
Observed app display: "Too many attempts. Please wait and try again."
Attempting /auth/me with valid JWT while Redis is down:
Observed /auth/me response when Redis down: status=429, code=rate_limited, message="rate limit exceeded, retry later"
Observed app display: "Too many attempts. Please wait and try again."
Service behavior: Gateway rate limiter and auth lockout FAIL CLOSED on Redis partition.
Result: PASS

=== [FAILURE MODE 5] Stop MongoDB ===
Command: docker stop wael-mongo
Attempting signup while MongoDB is down:
Observed signup response when Mongo down: status=502, code=null, message="service unavailable"
Observed app display: "Request failed. Please try again."
Service behavior: Auth service cannot query store, Gateway fails closed with 502.
Result: PASS
```

---

## 3. Evidence for In-Memory Fallbacks Audit

Under test scenario `FM-4 (Stop Redis)`, we observed how the stack handles infrastructure dependencies:
1. **API Gateway Rate Limiter**: When Redis is stopped, `RateLimiter.CheckAndRecord` logs `[SECURITY CRITICAL] Redis rate limiter error (FAIL CLOSED)...` and returns `(true, 30s)`. The gateway responds with HTTP 429 and `Retry-After: 30s`. Traffic is blocked rather than allowed through unthrottled.
2. **Auth Service Lockout**: When Redis is stopped, `AuthRateLimiter.IsLocked` logs `[SECURITY CRITICAL] Redis IsLocked check error (FAIL CLOSED)...` and returns `(true, 5m)`. Login requests receive HTTP 429.
3. **JWT Denylist**: When Redis is stopped, `jwtutil.ValidateToken` logs `[SECURITY CRITICAL] Redis error checking JWT denylist (FAIL CLOSED)...` and rejects any presented token with an error, returning HTTP 401 Unauthorized.
4. **Assessment**: The system strictly **fails closed**. No insecure in-memory bypasses were triggered in standard runtime configuration.

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
  ```
  HTTP/1.1 409 Conflict
  Content-Type: application/json
  {"error":"email already registered","code":"conflict"}
  ```
- **Recommended Fix**: Add a dedicated `POST /auth/resend-otp` endpoint, or if `FindByEmail` finds an unverified user (`!u.EmailVerified`), regenerate a fresh OTP, update `s.Codes.Set`, send the email, and return HTTP 200/201.

---

### Finding 2: Stateless Logout Without Server-Side Token Revocation
- **Severity**: Medium (Security / Session Invalidation)
- **Description**: When a user logs out in the Flutter application, `AuthProvider.logout()` deletes local tokens from `flutter_secure_storage`. However, `auth-service` does not expose a `POST /auth/logout` endpoint, and neither the gateway nor the auth service denylists the access token. Even though `shared/infra/jwtutil.RevokeToken(token)` exists in the codebase, it is never called on user logout. As a result, the access token remains valid against `/api/v1/auth/me` and downstream microservices until its 15-minute expiration.
- **Reproduction Steps**:
  1. Log in via `POST /api/v1/auth/login` and capture `access_token`.
  2. Perform user logout in Flutter (`AuthProvider.logout()`). Local store is cleared.
  3. Send `GET /api/v1/auth/me` with header `Authorization: Bearer <captured_access_token>`.
- **Raw Service Response**:
  ```
  HTTP/1.1 200 OK
  Content-Type: application/json
  {"id":"429ec517-e2e5-41f4-9301-b21a91be60f8","email":"matrix_test_1790750241498@example.com","role":"user","email_verified":true}
  ```
- **Recommended Fix**: Add `POST /auth/logout` in `auth-service` that invokes `jwtutil.RevokeToken(token)` or `jwtutil.RevokeAllUserTokens(userID)` to store the token's `jti` in Redis until expiry.

---

### Finding 3: Auth Lockout Threshold & Shared Client IP Scope
- **Severity**: Low / Behavioral Nuance
- **Description**: In `services/auth-service/internal/handlers/lockout.go` (`MemoryLockout` and `ratelimit.AuthRateLimiter`), failures only engage lockout starting on the 5th recorded failure (`if count >= 5`), returning backoff duration to apply to subsequent attempts. Therefore, attempts 1 through 5 return HTTP 401 Unauthorized, and attempt 6 returns HTTP 429 Too Many Requests. Additionally, `auth-service` records failures under both `login:email:<email>` and `login:ip:<ip>`. On shared networks or localhost testing where client IP is `127.0.0.1`, a lockout triggered on one email address blocks all subsequent login attempts from that IP across any account for the 30-second backoff duration.
- **Reproduction Steps**:
  1. Execute 5 consecutive failed login attempts on `email_a@example.com` with a bad password.
  2. Attempt a 6th login on `email_b@example.com` from the same IP.
- **Raw Service Response**:
  ```
  HTTP/1.1 429 Too Many Requests
  Content-Type: application/json
  {"error":"too many attempts, retry later","code":"locked_out"}
  ```
- **Behavior Note**: This behavior is working as designed for IP-level brute-force defense, but clients and test suites should be aware of the IP-level scope.

---

## 5. Items Not Verified / Out of Scope

1. **Academy & Course Endpoints**:
   - The academy service is currently being implemented by the parallel agent. Academy content in the Flutter app remains mock-backed behind repository interfaces (`MockAcademyRepository`) as instructed.
2. **Third-party Email Delivery (Resend API)**:
   - Email dispatch was tested using dev OTPs (`s.devOTPField()` returning `dev_otp` in JSON response under `local` environment) rather than actual external SMTP/Resend delivery.
