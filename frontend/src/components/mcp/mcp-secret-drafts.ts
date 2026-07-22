export type SecretKind = "parameter" | "header";

export interface SecretDraft {
  id: string;
  name: string;
  value: string;
  configured: boolean;
  keyHint?: string;
  originalName?: string;
}

let secretDraftSequence = 0;

export function nextSecretDraftID(kind: SecretKind) {
  secretDraftSequence += 1;
  return `${kind}-${secretDraftSequence}`;
}

export function sameSecretName(originalName: string | undefined, name: string, kind: SecretKind) {
  if (!originalName) return false;
  return kind === "header"
    ? originalName.toLowerCase() === name.toLowerCase()
    : originalName === name;
}
