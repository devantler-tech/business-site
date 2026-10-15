export const projects = ["Harbour", "Orchard", "Beacon", "Atlas", "Willow", "Compass"];
export const copy = {
  en: {
    title: "Built to work. Built to last.",
    intro: "Websites and applications with a clear scope and a considered next step.",
    projects: "Selected work",
    portal: "Client workspace",
    amount: "Invoice amount",
    save: "Check amount",
    result: "Amount checked",
    invalid: "Enter a positive amount using two decimal places.",
    language: "Dansk",
  },
  da: {
    title: "Bygget til at virke. Bygget til at holde.",
    intro: "Hjemmesider og applikationer med et klart omfang og et gennemtænkt næste skridt.",
    projects: "Udvalgt arbejde",
    portal: "Kundeportal",
    amount: "Fakturabeløb",
    save: "Kontrollér beløb",
    result: "Beløb kontrolleret",
    invalid: "Angiv et positivt beløb med to decimaler.",
    language: "English",
  },
};
export const localeOf = (value) => (value === "da" ? "da" : "en");
export const amountPattern = "(?:0|[1-9][0-9]{0,5})\\.[0-9]{2}";
export const validAmount = (value) =>
  typeof value === "string" && /^(?:0|[1-9][0-9]{0,5})\.[0-9]{2}$/.test(value) && Number(value) > 0;
