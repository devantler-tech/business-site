import { assertCompanyOutput } from "./company-output.mjs";

assertCompanyOutput("web/build",{
  identity:process.env.FEATURE_COMPANY_IDENTITY === "true",
  scope:true,
  template:process.env.PUBLIC_CONTENT_PROFILE === "production-template",
  address:process.env.COMPANY_POSTAL_ADDRESS ?? "",
});
console.log("Company/contact artifact validation and address confinement: PASS");
