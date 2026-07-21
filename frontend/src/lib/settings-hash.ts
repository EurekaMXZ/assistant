const settingsSections = [
  "user/profile",
  "user/personalization",
  "user/security",
  "user/expenses",
] as const;

export type SettingsSection = (typeof settingsSections)[number];

const SETTINGS_PREFIX = "#settings";

function buildSettingsHash(section: SettingsSection = "user/profile") {
  return `${SETTINGS_PREFIX}/${section}`;
}

export function buildSettingsUrl(section: SettingsSection = "user/profile") {
  return `/${buildSettingsHash(section)}`;
}

export function parseSettingsHash(hash: string): SettingsSection | null {
  const normalized = hash.startsWith("#") ? hash : `#${hash}`;
  const sectionPrefix = `${SETTINGS_PREFIX}/`;

  if (!normalized.startsWith(sectionPrefix)) {
    return null;
  }

  const section = normalized.slice(sectionPrefix.length);

  return settingsSections.includes(section as SettingsSection)
    ? (section as SettingsSection)
    : null;
}
