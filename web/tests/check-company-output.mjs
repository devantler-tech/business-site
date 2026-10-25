import { assertCompanyOutput } from "./company-output.mjs";

assertCompanyOutput("web/build","dist",{
  identity:process.env.FEATURE_COMPANY_IDENTITY === "true",
  scope:process.env.FEATURE_CUSTOMER_SCOPE === "true",
  template:process.env.PUBLIC_CONTENT_PROFILE === "production-template",
  address:process.env.COMPANY_POSTAL_ADDRESS ?? "",
});
console.log("Company/contact artifact parity and address confinement: PASS");
