/** Display-only input from the validated Go renderwire projection. */
export type ContactProjection = {
  locale: "en" | "da";
  name: string;
  email: string;
  company: string;
  revision: string;
  state: "normal" | "invalid" | "stale" | "verification";
  verification: "unverified" | "confirmable" | "verified";
  csrf: string;
  text: {
    title: string;
    intro: string;
    nameLabel: string;
    emailLabel: string;
    companyLabel: string;
    saveLabel: string;
    pendingText: string;
    workspaceLabel: string;
    verifyLabel: string;
    verificationHelp: string;
    errorText: string;
    reloadLabel: string;
  };
};
