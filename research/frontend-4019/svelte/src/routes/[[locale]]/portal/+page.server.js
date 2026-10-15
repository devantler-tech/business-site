import { error, fail } from "@sveltejs/kit";
import { validAmount } from "#sample/data.js";
export const load = ({ params, setHeaders }) => {
  if (params.locale && params.locale !== "da") error(404);
  setHeaders({ "cache-control": "no-store" });
  return { locale: params.locale || "en" };
};
export const actions = {
  default: async ({ request }) => {
    const amount = (await request.formData()).get("amount");
    return validAmount(amount) ? { ok: true, amount } : fail(400, { invalid: true });
  },
};
