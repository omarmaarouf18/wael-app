"use client";

import { useEffect, useState } from "react";

import { apiGet, getMe, getMyEntitlements, getMyRequests, type StudentRequestItem, type UserProfile } from "@/lib/api";
import { parseCatalog, type Catalog } from "@/lib/catalog-data";
import { dictionary, localePath, type Lang } from "@/lib/i18n";

import { useAuth } from "./auth-provider";
import styles from "./auth.module.css";
import { SiteShell } from "./site-shell";

interface AccountPageViewProps {
  lang: Lang;
}

interface AppConfig {
  update_url_android?: string;
  update_url_ios?: string;
  update_url?: string;
}

const DEFAULT_ANDROID_URL = "https://play.google.com/store/apps/details?id=app.elmetracademy.elmetr";
const DEFAULT_IOS_URL = "https://apps.apple.com/app/el-metr-academy/id6737155342";

export function AccountPageView({ lang }: AccountPageViewProps) {
  const t = dictionary(lang);
  const text = t.auth.account;
  const { isLoggedIn, isLoading: authLoading, logoutUser } = useAuth();

  const [loading, setLoading] = useState(true);
  const [profile, setProfile] = useState<UserProfile | null>(null);
  const [ownedSubjects, setOwnedSubjects] = useState<Array<{ id: string; title: string }>>([]);
  const [requests, setRequests] = useState<StudentRequestItem[]>([]);
  const [storeUrls, setStoreUrls] = useState({ android: DEFAULT_ANDROID_URL, ios: DEFAULT_IOS_URL });
  const [loggingOut, setLoggingOut] = useState(false);

  useEffect(() => {
    if (authLoading) return;
    if (!isLoggedIn) {
      window.location.href = localePath(lang, "/login");
      return;
    }

    let cancelled = false;

    async function loadData() {

      try {
        const [user, ents, reqs, catalogData, configData] = await Promise.all([
          getMe(),
          getMyEntitlements().catch(() => ({ subject_ids: [] as string[] })),
          getMyRequests().catch(() => ({ items: [] as StudentRequestItem[] })),
          apiGet<unknown>("/api/v1/academy/public/catalog").catch(() => null),
          apiGet<AppConfig>("/api/v1/academy/app-config").catch(() => ({} as AppConfig)),
        ]);

        if (cancelled) return;

        setProfile(user);
        setRequests(reqs.items || []);

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
            android: configData.update_url_android || configData.update_url || DEFAULT_ANDROID_URL,
            ios: configData.update_url_ios || DEFAULT_IOS_URL,
          });
        }
      } catch {
        // Handled via state
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
  }, [authLoading, isLoggedIn, lang]);

  const handleLogout = async () => {
    setLoggingOut(true);
    try {
      await logoutUser();
    } finally {
      window.location.href = localePath(lang, "/login");
    }
  };

  if (authLoading || loading) {
    return (
      <SiteShell lang={lang} path="/account">
        <div className={styles.accountContainer}>
          <div className={styles.card}>
            <p className={styles.lead}>{text.loading}</p>
          </div>
        </div>
      </SiteShell>
    );
  }

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

          <h2 className={styles.sectionTitle}>{text.profileSection}</h2>
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
              <a
                href={storeUrls.ios}
                target="_blank"
                rel="noopener noreferrer"
                className={styles.storeButton}
              >
                {text.downloadIos}
              </a>
            </div>
          </div>

          <h2 className={styles.sectionTitle}>{text.ownedSubjectsSection}</h2>
          {ownedSubjects.length === 0 ? (
            <div className={styles.emptyState}>
              <p>{text.noOwnedSubjects}</p>
              <div className={styles.browseLinkWrapper}>
                <a href={localePath(lang, "/subjects")} className={styles.link}>
                  {text.browseSubjects} →
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

          <h2 className={styles.sectionTitle}>{text.myRequestsSection}</h2>
          {requests.length === 0 ? (
            <p className={styles.emptyState}>{text.noRequests}</p>
          ) : (
            <ul className={styles.requestList}>
              {requests.map((r) => {
                const subjectTitle = lang === "ar" ? r.subject_title_ar || r.subject_id : r.subject_title_en || r.subject_title_ar || r.subject_id;
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
                      <span className={`${styles.statusBadge} ${badgeClass}`}>{badgeText}</span>
                    </div>
                    <div className={styles.requestMeta}>
                      <span>{text.requestDate(formatRequestDate(r.created_at, lang))}</span>
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
      </div>
    </SiteShell>
  );
}

function formatRequestDate(iso: string, lang: Lang): string {
  try {
    const d = new Date(iso);
    return d.toLocaleDateString(lang === "ar" ? "ar-EG" : "en-US", {
      year: "numeric",
      month: "short",
      day: "numeric",
    });
  } catch {
    return iso;
  }
}

