// Confirmed public invitation only (#149), not server-side order authorization.
// Outside-EU obligations and portal enforcement remain open under #147 and #146.
export const customerScope = {
  en: {
    title: 'Who I work with',
    audience: 'Private customers in Denmark and the rest of the EU, and businesses in Denmark.',
    boundary: 'I cannot currently take on projects for businesses in other EU countries under the PMV setup. Work outside the EU is not open while its requirements are being confirmed.',
  },
  da: {
    title: 'Hvem jeg samarbejder med',
    audience: 'Privatkunder i Danmark og resten af EU samt virksomheder i Danmark.',
    boundary: 'Under PMV-ordningen kan jeg foreløbig ikke påtage mig projekter for virksomheder i andre EU-lande. Opgaver uden for EU er ikke åbne, mens kravene afklares.',
  },
} as const;
