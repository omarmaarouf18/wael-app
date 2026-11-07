"use client";

import { useEffect, useState } from "react";

import {
  ApiError,
  apiGet,
  changePassword,
  deleteSession,
  getMe,
  getMyEntitlements,
  getMyOrders,
  getMyRequests,
  getNotifications,
  getSessions,
  markNotificationRead,
  requestAccountDeletion,
  updateProfile,
  type DeviceSession,
  type NotificationItem,
  type OrderResponse,
  type StudentRequestItem,
  type UserProfile,
} from "@/lib/api";
import { parseCatalog, type Catalog } from "@/lib/catalog-data";
import { dictionary, localePath, type Lang } from "@/lib/i18n";

import { useAuth } from "./auth-provider";
import { STORE_URLS } from "@/lib/store-links";

import styles from "./auth.module.css";
import { SiteShell } from "./site-shell";

interface AccountPageViewProps {
  lang: Lang;
}

interface AppConfig {
  update_url_android?: string;
  update_url?: string;
}


type AccountTab = "overview" | "orders" | "devices" | "notifications" | "password" | "danger";

export function AccountPageView({ lang }: AccountPageViewProps) {
  const t = dictionary(lang);
  const text = t.auth.account;
  const hubText = t.accountHub;
  const { isLoggedIn, isLoading: authLoading, logoutUser } = useAuth();

  const [activeTab, setActiveTab] = useState<AccountTab>("overview");
  const [loading, setLoading] = useState(true);
  // Network or server failures (added 2026-11-07): the profile could not be
  // read at all (loadError), or some lists could not (partialError). Before,
  // both looked like empty lists.
  const [loadError, setLoadError] = useState(false);
  const [partialError, setPartialError] = useState(false);
  const [reloadKey, setReloadKey] = useState(0);
  const [loggingOut, setLoggingOut] = useState(false);

  // User and core data
  const [profile, setProfile] = useState<UserProfile | null>(null);
  const [ownedSubjects, setOwnedSubjects] = useState<Array<{ id: string; title: string }>>([]);
  const [orders, setOrders] = useState<OrderResponse[]>([]);
  const [requests, setRequests] = useState<StudentRequestItem[]>([]);
  const [sessions, setSessions] = useState<DeviceSession[]>([]);
  const [notifications, setNotifications] = useState<NotificationItem[]>([]);
  const [storeUrls, setStoreUrls] = useState<{ android: string }>({ android: STORE_URLS.android });

  // Profile Edit Modal State
  const [isEditingProfile, setIsEditingProfile] = useState(false);
  const [editName, setEditName] = useState("");
  const [editPhone, setEditPhone] = useState("");
  const [editPassword, setEditPassword] = useState("");
  const [editLoading, setEditLoading] = useState(false);
  const [editError, setEditError] = useState<string | null>(null);
  const [editSuccess, setEditSuccess] = useState<string | null>(null);

  // Change Password State
  const [currentPassword, setCurrentPassword] = useState("");
  const [newPassword, setNewPassword] = useState("");
  const [confirmPassword, setConfirmPassword] = useState("");
  const [passwordLoading, setPasswordLoading] = useState(false);
  const [passwordError, setPasswordError] = useState<string | null>(null);
  const [passwordSuccess, setPasswordSuccess] = useState<string | null>(null);

  // Delete Account State
  const [deletePassword, setDeletePassword] = useState("");
  const [deleteConfirm, setDeleteConfirm] = useState("");
  const [deleteLoading, setDeleteLoading] = useState(false);
  const [deleteError, setDeleteError] = useState<string | null>(null);
  const [scheduledPurgeDate, setScheduledPurgeDate] = useState<string | null>(null);

  // Sessions action state
  const [signingOutSid, setSigningOutSid] = useState<string | null>(null);
  const [sessionError, setSessionError] = useState<string | null>(null);

  // Notifications action state
  const [markingReadId, setMarkingReadId] = useState<string | null>(null);

  useEffect(() => {
    if (authLoading) return;
    if (!isLoggedIn) {
      window.location.assign(localePath(lang, "/login"));
      return;
    }

    let cancelled = false;

    async function loadData() {
      let someFailed = false;
      const fallback = <T,>(value: T) => () => {
        someFailed = true;
        return value;
      };
      try {
        const [user, ents, ordersRes, reqs, sessRes, notifsRes, catalogData, configData] =
          await Promise.all([
            getMe(),
            getMyEntitlements().catch(fallback({ subject_ids: [] as string[] })),
            getMyOrders().catch(fallback({ items: [] as OrderResponse[] })),
            getMyRequests().catch(fallback({ items: [] as StudentRequestItem[] })),
            getSessions().catch(fallback({ sessions: [] as DeviceSession[] })),
            getNotifications().catch(fallback({ notifications: [] as NotificationItem[] })),
            apiGet<unknown>("/api/v1/academy/public/catalog").catch(() => null),
            apiGet<AppConfig>("/api/v1/academy/app-config").catch(() => ({} as AppConfig)),
          ]);

        if (cancelled) return;

        setLoadError(false);
        setPartialError(someFailed);
        setProfile(user);
        setEditName(user.full_name || "");
        setEditPhone(user.phone || "");
        setOrders(ordersRes.items || []);
        setRequests(reqs.items || []);
        setSessions(sessRes.sessions || []);
        setNotifications(notifsRes.notifications || []);

        // Resolve subject titles from public catalog
        const catalog: Catalog | null = catalogData ? parseCatalog(catalogData) : null;
        const resolved: Array<{ id: string; title: string }> = [];

        for (const id of ents.subject_ids) {
          let title = id;
          if (catalog) {
            for (const st of catalog.studyTypes) {
              for (const lvl of st.levels) {
                const found = lvl.subjects.find((s) => s.id === id);
                if (found) {
                  title = lang === "ar" ? found.title.ar : found.title.en || found.title.ar;
                  break;
                }
              }
            }
          }
          resolved.push({ id, title });
        }
        setOwnedSubjects(resolved);

        if (configData) {
          setStoreUrls({
            android: configData.update_url_android || configData.update_url || STORE_URLS.android,
          });
        }
      } catch {
        if (!cancelled) setLoadError(true);
      } finally {
        if (!cancelled) {
          setLoading(false);
        }
      }
    }

    void loadData();

    return () => {
      cancelled = true;
    };
  }, [authLoading, isLoggedIn, lang, reloadKey]);

  const retryLoad = () => {
    setLoading(true);
    setReloadKey((n) => n + 1);
  };

  const handleLogout = async () => {
    setLoggingOut(true);
    try {
      await logoutUser();
    } finally {
      window.location.assign(localePath(lang, "/login"));
    }
  };

  const handleProfileSubmit = async (e: React.FormEvent) => {
    e.preventDefault();
    setEditError(null);
    setEditSuccess(null);

    if (!editPassword) {
      setEditError(hubText.profile.errorWrongPassword);
      return;
    }

    setEditLoading(true);

    try {
      const payload: { full_name?: string; phone?: string; current_password: string } = {
        current_password: editPassword,
      };
      if (editName.trim() !== (profile?.full_name || "")) {
        payload.full_name = editName.trim();
      }
      if (editPhone.trim() !== (profile?.phone || "")) {
        payload.phone = editPhone.trim();
      }

      const updated = await updateProfile(payload);
      setProfile((prev) =>
        prev
          ? {
              ...prev,
              full_name: updated.full_name || prev.full_name,
              phone: updated.phone ?? prev.phone,
            }
          : null,
      );
      setEditPassword("");
      setEditSuccess(hubText.profile.success);
      setTimeout(() => {
        setIsEditingProfile(false);
        setEditSuccess(null);
      }, 1500);
    } catch (err: unknown) {
      if (err instanceof ApiError) {
        if (err.code === "change_too_soon") {
          const days = err.retryAfter ? Math.ceil(err.retryAfter / 86400) : 30;
          setEditError(hubText.profile.errorTooSoon(days));
        } else if (err.code === "conflict" || err.code === "phone_exists") {
          setEditError(hubText.profile.errorConflict);
        } else if (err.code === "invalid_credentials" || err.status === 401) {
          setEditError(hubText.profile.errorWrongPassword);
        } else {
          setEditError(err.message || text.loading);
        }
      } else {
        setEditError(text.loading);
      }
    } finally {
      setEditLoading(false);
    }
  };

  const handlePasswordChange = async (e: React.FormEvent) => {
    e.preventDefault();
    setPasswordError(null);
    setPasswordSuccess(null);

    if (newPassword.length < 8 || newPassword.length > 72) {
      setPasswordError(hubText.password.lead);
      return;
    }

    if (newPassword !== confirmPassword) {
      setPasswordError(hubText.password.mismatch);
      return;
    }

    setPasswordLoading(true);

    try {
      await changePassword({
        current_password: currentPassword,
        new_password: newPassword,
      });

      setPasswordSuccess(hubText.password.success);
      setCurrentPassword("");
      setNewPassword("");
      setConfirmPassword("");
    } catch (err: unknown) {
      if (err instanceof ApiError) {
        if (err.code === "invalid_credentials" || err.status === 401) {
          setPasswordError(hubText.profile.errorWrongPassword);
        } else {
          setPasswordError(err.message || hubText.password.mismatch);
        }
      } else {
        setPasswordError(hubText.password.mismatch);
      }
    } finally {
      setPasswordLoading(false);
    }
  };

  const handleSignOutSession = async (sid: string, isCurrent: boolean) => {
    setSigningOutSid(sid);
    setSessionError(null);

    try {
      await deleteSession(sid);
      if (isCurrent) {
        await logoutUser();
        window.location.assign(localePath(lang, "/login"));
        return;
      }
      setSessions((prev) => prev.filter((s) => s.sid !== sid));
    } catch (err: unknown) {
      if (err instanceof ApiError) {
        setSessionError(err.message || "Failed to sign out session");
      }
    } finally {
      setSigningOutSid(null);
    }
  };

  const handleMarkNotificationRead = async (id: string) => {
    setMarkingReadId(id);
    try {
      await markNotificationRead(id);
      setNotifications((prev) =>
        prev.map((n) => (n.id === id ? { ...n, read: true } : n)),
      );
    } catch {
      // Ignore
    } finally {
      setMarkingReadId(null);
    }
  };

  const isDeleteConfirmationValid = () => {
    if (!deletePassword) return false;
    const trimmed = deleteConfirm.trim();
    if (trimmed === "حذف") return true;
    if (lang === "en" && trimmed.toLowerCase() === "delete") return true;
    return false;
  };

  const handleDeleteAccount = async (e: React.FormEvent) => {
    e.preventDefault();
    if (!isDeleteConfirmationValid()) return;

    setDeleteLoading(true);
    setDeleteError(null);

    try {
      const res = await requestAccountDeletion({
        current_password: deletePassword,
        confirm: deleteConfirm.trim(),
      });

      const purgeDate = res.deletion_date || new Date().toISOString();
      setScheduledPurgeDate(purgeDate);

      // Perform logout per spec
      await logoutUser();

      setTimeout(() => {
        window.location.assign(localePath(lang, "/login"));
      }, 4000);
    } catch (err: unknown) {
      if (err instanceof ApiError) {
        if (err.code === "invalid_credentials" || err.status === 401) {
          setDeleteError(hubText.profile.errorWrongPassword);
        } else {
          setDeleteError(err.message || "Failed to request account deletion");
        }
      } else {
        setDeleteError("Failed to request account deletion");
      }
    } finally {
      setDeleteLoading(false);
    }
  };

  const isWebSession = (label: string): boolean => {
    const l = label.toLowerCase();
    return (
      l.includes("web") ||
      l.includes("chrome") ||
      l.includes("firefox") ||
      l.includes("safari") ||
      l.includes("edge") ||
      l.includes("browser")
    );
  };

  if (authLoading || loading) {
    return (
      <SiteShell lang={lang} path="/account">
        <div className={styles.accountContainer}>
          <div className={styles.card} aria-busy="true">
            <h1 className={styles.visuallyHidden}>{text.title}</h1>
            <p className={styles.visuallyHidden} role="status">
              {text.loading}
            </p>
            <div className={styles.skeleton} aria-hidden="true">
              <span className={`${styles.skeletonLine} ${styles.skeletonLineWide}`} />
              <span className={`${styles.skeletonLine} ${styles.skeletonLineShort}`} />
              <span className={styles.skeletonLine} />
              <span className={styles.skeletonBlock} />
              <span className={styles.skeletonBlock} />
            </div>
          </div>
        </div>
      </SiteShell>
    );
  }

  if (loadError) {
    return (
      <SiteShell lang={lang} path="/account">
        <div className={styles.accountContainer}>
          <div className={styles.card}>
            <h1 className={styles.title}>{text.title}</h1>
            <div className={styles.errorState} role="alert">
              <p>{t.common.networkError}</p>
              <button type="button" onClick={retryLoad} className={styles.secondaryButton}>
                {t.common.retry}
              </button>
            </div>
          </div>
        </div>
      </SiteShell>
    );
  }

  const unreadNotificationsCount = notifications.filter((n) => !n.read).length;

  return (
    <SiteShell lang={lang} path="/account">
      <div className={styles.accountContainer}>
        <div className={styles.card}>
          <div className={styles.headerActions}>
            <div>
              <h1 className={styles.title}>{text.title}</h1>
              <p className={styles.lead}>{text.lead}</p>
            </div>
            <button
              type="button"
              onClick={handleLogout}
              disabled={loggingOut}
              className={styles.secondaryButton}
            >
              {loggingOut ? text.loggingOut : text.logout}
            </button>
          </div>

          {partialError ? (
            <div className={`${styles.errorState} ${styles.bannerSpaced}`} role="alert">
              <p>{t.common.partialLoad}</p>
              <button type="button" onClick={retryLoad} className={styles.secondaryButton}>
                {t.common.retry}
              </button>
            </div>
          ) : null}

          {/* Navigation Tabs */}
          <nav className={styles.tabNav} aria-label="Account Tabs">
            <button
              type="button"
              onClick={() => setActiveTab("overview")}
              aria-pressed={activeTab === "overview"}
              className={`${styles.tabButton} ${activeTab === "overview" ? styles.tabButtonActive : ""}`}
            >
              {hubText.tabs.overview}
            </button>
            <button
              type="button"
              onClick={() => setActiveTab("orders")}
              aria-pressed={activeTab === "orders"}
              className={`${styles.tabButton} ${activeTab === "orders" ? styles.tabButtonActive : ""}`}
            >
              {hubText.tabs.orders} {orders.length > 0 ? `(${orders.length})` : ""}
            </button>
            <button
              type="button"
              onClick={() => setActiveTab("devices")}
              aria-pressed={activeTab === "devices"}
              className={`${styles.tabButton} ${activeTab === "devices" ? styles.tabButtonActive : ""}`}
            >
              {hubText.tabs.devices} {sessions.length > 0 ? `(${sessions.length})` : ""}
            </button>
            <button
              type="button"
              onClick={() => setActiveTab("notifications")}
              aria-pressed={activeTab === "notifications"}
              className={`${styles.tabButton} ${activeTab === "notifications" ? styles.tabButtonActive : ""}`}
            >
              {hubText.tabs.notifications}{" "}
              {unreadNotificationsCount > 0 ? `(${unreadNotificationsCount})` : ""}
            </button>
            <button
              type="button"
              onClick={() => setActiveTab("password")}
              aria-pressed={activeTab === "password"}
              className={`${styles.tabButton} ${activeTab === "password" ? styles.tabButtonActive : ""}`}
            >
              {hubText.tabs.password}
            </button>
            <button
              type="button"
              onClick={() => setActiveTab("danger")}
              aria-pressed={activeTab === "danger"}
              className={`${styles.tabButton} ${activeTab === "danger" ? styles.tabButtonActive : ""}`}
            >
              {hubText.tabs.danger}
            </button>
          </nav>

          {/* Tab 1: Overview & My Subjects */}
          {activeTab === "overview" && (
            <div>
              <div className={styles.sectionHeaderRow}>
                <h2 className={`${styles.sectionTitle} ${styles.sectionTitleBare}`}>
                  {text.profileSection}
                </h2>
                <button
                  type="button"
                  onClick={() => setIsEditingProfile(true)}
                  className={styles.secondaryButton}
                >
                  ✎ {hubText.profile.editBtn}
                </button>
              </div>

              <div className={styles.profileGrid}>
                <div className={styles.profileItem}>
                  <span className={styles.profileLabel}>{text.fullName}</span>
                  <span className={styles.profileValue}>{profile?.full_name}</span>
                </div>
                <div className={styles.profileItem}>
                  <span className={styles.profileLabel}>{text.email}</span>
                  <span className={styles.profileValue} dir="ltr">
                    {profile?.email}
                  </span>
                </div>
                <div className={styles.profileItem}>
                  <span className={styles.profileLabel}>{text.phone}</span>
                  <span className={styles.profileValue} dir="ltr">
                    {profile?.phone || text.noPhone}
                  </span>
                </div>
                <div className={styles.profileItem}>
                  <span className={styles.profileLabel}>{text.role}</span>
                  <span className={styles.profileValue}>{text.studentRole}</span>
                </div>
              </div>

              <div className={styles.studyBanner}>
                <h3 className={styles.studyBannerTitle}>{text.studyBannerTitle}</h3>
                <p className={styles.studyBannerLead}>{text.studyBannerLead}</p>
                <div className={styles.storeButtons}>
                  <a
                    href={storeUrls.android}
                    target="_blank"
                    rel="noopener noreferrer"
                    className={styles.storeButton}
                  >
                    {text.downloadAndroid}
                  </a>
                </div>
              </div>

              <h2 className={styles.sectionTitle}>{text.ownedSubjectsSection}</h2>
              {ownedSubjects.length === 0 ? (
                <div className={styles.emptyState}>
                  <p>{text.noOwnedSubjects}</p>
                  <div className={styles.browseLinkWrapper}>
                    <a href={localePath(lang, "/subjects")} className={styles.link}>
                      {text.browseSubjects}
                    </a>
                  </div>
                </div>
              ) : (
                <ul className={styles.subjectList}>
                  {ownedSubjects.map((s) => (
                    <li key={s.id} className={styles.subjectItem}>
                      <span className={styles.subjectTitle}>{s.title}</span>
                      <a href={localePath(lang, `/subjects/${s.id}`)} className={styles.link}>
                        {t.catalog.details}
                      </a>
                    </li>
                  ))}
                </ul>
              )}
            </div>
          )}

          {/* Tab 2: Orders & Requests */}
          {activeTab === "orders" && (
            <div>
              <h2 className={styles.sectionTitle}>{hubText.orders.title}</h2>
              <p className={styles.lead}>{hubText.orders.lead}</p>

              {orders.length === 0 && requests.length === 0 ? (
                <p className={styles.emptyState}>{hubText.orders.noOrders}</p>
              ) : orders.length > 0 ? (
                orders.map((order) => {
                  const paymentMethodLabel =
                    order.payment_method === "instapay"
                      ? hubText.orders.methodInstapay
                      : hubText.orders.methodWallet;
                  return (
                    <div key={order.id} className={styles.orderCard}>
                      <div className={styles.orderCardHeader}>
                        <div>
                          <strong>{hubText.orders.orderNumber(order.id)}</strong>
                          <span className={styles.orderDate}>
                            {hubText.orders.orderDate(formatDate(order.created_at, lang))}
                          </span>
                        </div>
                        <span
                          className={`${styles.statusBadge} ${order.status === "open" ? styles.statusPending : styles.statusAccepted}`}
                        >
                          {order.status === "open" ? "مفتوح" : "مغلق"}
                        </span>
                      </div>

                      <div className={styles.requestMeta}>
                        {order.payment_method && (
                          <span>
                            {hubText.orders.paymentMethod(paymentMethodLabel)}
                          </span>
                        )}
                        {order.transfer_reference && (
                          <span>
                            {hubText.orders.transferRef(order.transfer_reference)}
                          </span>
                        )}
                        {order.sender_number && (
                          <span>
                            {hubText.orders.senderNumber(order.sender_number)}
                          </span>
                        )}
                        {order.amount_paid !== undefined && (
                          <span>
                            {hubText.orders.amountPaid(order.amount_paid)}
                          </span>
                        )}
                        <span>{hubText.orders.total(order.total)}</span>
                      </div>

                      {order.items && order.items.length > 0 && (
                        <table className={styles.orderItemsTable}>
                          <thead>
                            <tr>
                              <th>{lang === "ar" ? "المادة" : "Subject"}</th>
                              <th>{lang === "ar" ? "السعر" : "Price"}</th>
                              <th>{lang === "ar" ? "الحالة" : "Status"}</th>
                            </tr>
                          </thead>
                          <tbody>
                            {order.items.map((item) => {
                              const itemTitle =
                                lang === "ar"
                                  ? item.title_ar
                                  : item.title_en || item.title_ar;
                              let badgeClass = styles.statusPending;
                              let badgeText = hubText.orders.statusPending;
                              if (item.status === "accepted") {
                                badgeClass = styles.statusAccepted;
                                badgeText = hubText.orders.statusAccepted;
                              } else if (item.status === "rejected") {
                                badgeClass = styles.statusRejected;
                                badgeText = hubText.orders.statusRejected;
                              }
                              return (
                                <tr key={item.request_id || item.subject_id}>
                                  <td>
                                    <div>{itemTitle}</div>
                                    {item.status === "rejected" && item.reject_reason && (
                                      <div className={styles.rejectReasonBox}>
                                        {hubText.orders.rejectReason(item.reject_reason)}
                                      </div>
                                    )}
                                  </td>
                                  <td>{item.price} ج.م</td>
                                  <td>
                                    <span className={`${styles.statusBadge} ${badgeClass}`}>
                                      {badgeText}
                                    </span>
                                  </td>
                                </tr>
                              );
                            })}
                          </tbody>
                        </table>
                      )}
                    </div>
                  );
                })
              ) : (
                /* Fallback to single requests */
                <ul className={styles.requestList}>
                  {requests.map((r) => {
                    const subjectTitle =
                      lang === "ar"
                        ? r.subject_title_ar || r.subject_id
                        : r.subject_title_en || r.subject_title_ar || r.subject_id;
                    let badgeClass = styles.statusPending;
                    let badgeText = text.requestStatusPending;
                    if (r.status === "accepted") {
                      badgeClass = styles.statusAccepted;
                      badgeText = text.requestStatusAccepted;
                    } else if (r.status === "rejected") {
                      badgeClass = styles.statusRejected;
                      badgeText = text.requestStatusRejected;
                    }
                    return (
                      <li key={r.id} className={styles.requestItem}>
                        <div className={styles.requestHeader}>
                          <span className={styles.subjectTitle}>{subjectTitle}</span>
                          <span className={`${styles.statusBadge} ${badgeClass}`}>
                            {badgeText}
                          </span>
                        </div>
                        <div className={styles.requestMeta}>
                          <span>{text.requestDate(formatDate(r.created_at, lang))}</span>
                          <span>{text.requestAmount(String(r.amount || 0))}</span>
                        </div>
                        {r.status === "rejected" && r.reject_reason ? (
                          <div className={styles.rejectReasonBox}>
                            {text.requestRejectReason(r.reject_reason)}
                          </div>
                        ) : null}
                      </li>
                    );
                  })}
                </ul>
              )}
            </div>
          )}

          {/* Tab 3: Devices & Sessions */}
          {activeTab === "devices" && (
            <div>
              <h2 className={styles.sectionTitle}>{hubText.devices.title}</h2>
              <p className={styles.lead}>{hubText.devices.lead}</p>

              {sessionError && <div className={styles.errorBanner} role="alert">{sessionError}</div>}

              {sessions.length === 0 ? (
                <p className={styles.emptyState}>{hubText.devices.noSessions}</p>
              ) : (
                <ul className={styles.requestList}>
                  {sessions.map((s) => {
                    const web = isWebSession(s.device_label);
                    const isBusy = signingOutSid === s.sid;
                    return (
                      <li key={s.sid} className={styles.requestItem}>
                        <div className={styles.requestHeader}>
                          <div className={styles.titleWithBadges}>
                            <span className={styles.subjectTitle}>{s.device_label}</span>
                            {web && (
                              <span className={`${styles.badge} ${styles.badgeWeb}`}>
                                {hubText.devices.webBadge}
                              </span>
                            )}
                            {s.current && (
                              <span className={`${styles.badge} ${styles.badgeCurrent}`}>
                                {hubText.devices.currentBadge}
                              </span>
                            )}
                          </div>

                          <button
                            type="button"
                            disabled={isBusy}
                            onClick={() => handleSignOutSession(s.sid, s.current)}
                            className={`${styles.secondaryButton} ${styles.secondaryButtonDanger}`}
                          >
                            {isBusy ? hubText.devices.signingOut : hubText.devices.signOut}
                          </button>
                        </div>

                        <div className={styles.requestMeta}>
                          <span>
                            {hubText.devices.lastUsed(formatDate(s.last_used_at, lang))}
                          </span>
                          <span>
                            {hubText.devices.created(formatDate(s.created_at, lang))}
                          </span>
                        </div>
                        {s.current && (
                          <span className={styles.helperText}>
                            {hubText.devices.signOutCurrentWarning}
                          </span>
                        )}
                      </li>
                    );
                  })}
                </ul>
              )}
            </div>
          )}

          {/* Tab 4: Notifications Inbox */}
          {activeTab === "notifications" && (
            <div>
              <h2 className={styles.sectionTitle}>{hubText.notifications.title}</h2>
              <p className={styles.lead}>{hubText.notifications.lead}</p>

              {notifications.length === 0 ? (
                <p className={styles.emptyState}>{hubText.notifications.noNotifications}</p>
              ) : (
                <ul className={styles.requestList}>
                  {notifications.map((n) => {
                    const title = lang === "ar" ? n.title_ar || n.title : n.title || n.title_ar;
                    const body = lang === "ar" ? n.body_ar || n.body : n.body || n.body_ar;
                    const isMarking = markingReadId === n.id;
                    return (
                      <li
                        key={n.id}
                        className={`${styles.requestItem} ${!n.read ? styles.requestItemUnread : ""}`}
                      >
                        <div className={styles.requestHeader}>
                          <div className={styles.titleWithBadges}>
                            <span className={styles.subjectTitle}>{title}</span>
                            {!n.read && (
                              <span className={`${styles.badge} ${styles.badgeUnread}`}>
                                {hubText.notifications.unreadBadge}
                              </span>
                            )}
                          </div>

                          {!n.read && (
                            <button
                              type="button"
                              disabled={isMarking}
                              onClick={() => handleMarkNotificationRead(n.id)}
                              className={styles.secondaryButton}
                            >
                              {isMarking
                                ? hubText.notifications.markingRead
                                : hubText.notifications.markRead}
                            </button>
                          )}
                        </div>

                        <p className={styles.notificationBody}>
                          {body}
                        </p>

                        <div className={styles.requestMeta}>
                          <span>{formatDate(n.created_at, lang)}</span>
                        </div>
                      </li>
                    );
                  })}
                </ul>
              )}
            </div>
          )}

          {/* Tab 5: Change Password */}
          {activeTab === "password" && (
            <div>
              <h2 className={styles.sectionTitle}>{hubText.password.title}</h2>
              <p className={styles.lead}>{hubText.password.lead}</p>

              <form onSubmit={handlePasswordChange} className={`${styles.form} ${styles.formNarrow}`}>
                {passwordError && <div className={styles.errorBanner} role="alert">{passwordError}</div>}
                {passwordSuccess && <div className={styles.successBanner} role="status">{passwordSuccess}</div>}

                <div className={styles.field}>
                  <label htmlFor="current-pw" className={styles.label}>
                    {hubText.password.currentPassword}
                  </label>
                  <input
                    id="current-pw"
                    type="password"
                    required
                    value={currentPassword}
                    onChange={(e) => setCurrentPassword(e.target.value)}
                    className={styles.input}
                    dir="ltr"
                  />
                </div>

                <div className={styles.field}>
                  <label htmlFor="new-pw" className={styles.label}>
                    {hubText.password.newPassword}
                  </label>
                  <input
                    id="new-pw"
                    type="password"
                    required
                    minLength={8}
                    maxLength={72}
                    value={newPassword}
                    onChange={(e) => setNewPassword(e.target.value)}
                    className={styles.input}
                    dir="ltr"
                  />
                </div>

                <div className={styles.field}>
                  <label htmlFor="confirm-pw" className={styles.label}>
                    {hubText.password.confirmNewPassword}
                  </label>
                  <input
                    id="confirm-pw"
                    type="password"
                    required
                    minLength={8}
                    maxLength={72}
                    value={confirmPassword}
                    onChange={(e) => setConfirmPassword(e.target.value)}
                    className={styles.input}
                    dir="ltr"
                  />
                </div>

                <button
                  type="submit"
                  disabled={passwordLoading}
                  className={styles.submit}
                >
                  {passwordLoading ? hubText.password.submitting : hubText.password.submit}
                </button>

                <div className={styles.stackGapTop}>
                  <a href={localePath(lang, "/forgot-password")} className={styles.link}>
                    {hubText.password.forgotLink}
                  </a>
                </div>
              </form>
            </div>
          )}

          {/* Tab 6: Delete Account */}
          {activeTab === "danger" && (
            <div>
              <h2 className={`${styles.sectionTitle} ${styles.sectionTitleDanger}`}>
                {hubText.deleteAccount.title}
              </h2>

              <div className={styles.warningBox}>
                {hubText.deleteAccount.warning}
              </div>

              {scheduledPurgeDate ? (
                <div className={styles.successBanner} role="status">
                  {hubText.deleteAccount.scheduledNotice(formatDate(scheduledPurgeDate, lang))}
                </div>
              ) : (
                <form onSubmit={handleDeleteAccount} className={`${styles.form} ${styles.formNarrow}`}>
                  {deleteError && <div className={styles.errorBanner} role="alert">{deleteError}</div>}

                  <p className={styles.bodyText}>
                    {hubText.deleteAccount.confirmPrompt}
                  </p>

                  <div className={styles.field}>
                    <label htmlFor="delete-confirm" className={styles.label}>
                      {lang === "ar" ? 'اكتب كلمة "حذف"' : 'Type "delete"'}
                    </label>
                    <input
                      id="delete-confirm"
                      type="text"
                      required
                      placeholder={hubText.deleteAccount.confirmPlaceholder}
                      value={deleteConfirm}
                      onChange={(e) => setDeleteConfirm(e.target.value)}
                      className={styles.input}
                    />
                  </div>

                  <div className={styles.field}>
                    <label htmlFor="delete-password" className={styles.label}>
                      {hubText.deleteAccount.currentPasswordLabel}
                    </label>
                    <input
                      id="delete-password"
                      type="password"
                      required
                      value={deletePassword}
                      onChange={(e) => setDeletePassword(e.target.value)}
                      className={styles.input}
                      dir="ltr"
                    />
                  </div>

                  <button
                    type="submit"
                    disabled={deleteLoading || !isDeleteConfirmationValid()}
                    className={styles.dangerButton}
                  >
                    {deleteLoading
                      ? hubText.deleteAccount.submitting
                      : hubText.deleteAccount.submit}
                  </button>
                </form>
              )}
            </div>
          )}
        </div>
      </div>

      {/* Profile Edit Modal */}
      {isEditingProfile && (
        <div className={styles.modalOverlay} onClick={() => setIsEditingProfile(false)}>
          <div className={styles.modalDialog} onClick={(e) => e.stopPropagation()}>
            <h3 className={`${styles.title} ${styles.titleSmall}`}>
              {hubText.profile.editTitle}
            </h3>

            {editError && <div className={`${styles.errorBanner} ${styles.bannerSpaced}`} role="alert">{editError}</div>}
            {editSuccess && <div className={`${styles.successBanner} ${styles.bannerSpaced}`} role="status">{editSuccess}</div>}

            <form onSubmit={handleProfileSubmit} className={styles.form}>
              <div className={styles.field}>
                <label htmlFor="edit-name" className={styles.label}>
                  {hubText.profile.nameLabel}
                </label>
                <input
                  id="edit-name"
                  type="text"
                  required
                  value={editName}
                  onChange={(e) => setEditName(e.target.value)}
                  className={styles.input}
                />
              </div>

              <div className={styles.field}>
                <label htmlFor="edit-phone" className={styles.label}>
                  {hubText.profile.phoneLabel}
                </label>
                <input
                  id="edit-phone"
                  type="text"
                  placeholder={hubText.profile.phonePlaceholder}
                  value={editPhone}
                  onChange={(e) => setEditPhone(e.target.value)}
                  className={styles.input}
                  dir="ltr"
                />
                <span className={styles.helperText}>
                  {hubText.profile.phoneHelper}
                </span>
              </div>

              <div className={styles.field}>
                <label htmlFor="edit-password" className={styles.label}>
                  {hubText.profile.currentPasswordLabel}
                </label>
                <input
                  id="edit-password"
                  type="password"
                  required
                  value={editPassword}
                  onChange={(e) => setEditPassword(e.target.value)}
                  className={styles.input}
                  dir="ltr"
                />
              </div>

              <div className={styles.buttonRow}>
                <button
                  type="submit"
                  disabled={editLoading}
                  className={`${styles.submit} ${styles.grow}`}
                >
                  {editLoading ? hubText.profile.saving : hubText.profile.save}
                </button>
                <button
                  type="button"
                  onClick={() => setIsEditingProfile(false)}
                  className={styles.secondaryButton}
                >
                  {hubText.profile.cancel}
                </button>
              </div>
            </form>
          </div>
        </div>
      )}
    </SiteShell>
  );
}

function formatDate(iso: string, lang: Lang): string {
  try {
    const d = new Date(iso);
    return d.toLocaleDateString(lang === "ar" ? "ar-EG" : "en-US", {
      year: "numeric",
      month: "short",
      day: "numeric",
      hour: "2-digit",
      minute: "2-digit",
    });
  } catch {
    return iso;
  }
}
