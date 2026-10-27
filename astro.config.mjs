import sitemap from "@astrojs/sitemap";
import starlight from "@astrojs/starlight";
import mermaid from "astro-mermaid";
import { defineConfig, envField } from "astro/config";
import starlightBlog from "starlight-blog";
import starlightGithubAlerts from "starlight-github-alerts";
import starlightLinksValidator from "starlight-links-validator";
import { readFileSync } from "node:fs";
import { journalConfig } from "./src/components/business/journal-config.js";

export default defineConfig({
  site: "https://devantler.tech",
  // Keep each framework consumer's declared YAML version when prerender chunks
  // are relocated. A bare external import would resolve the site's newer major.
  vite: {
    environments: {
      prerender: { resolve: { noExternal: ["js-yaml"] } },
    },
  },
  // The renamed tenant-template URL has an authored redirect page in src/pages/
  // so incoming heading bookmarks survive the static publication.
  // Build-time feature flags (feature-flag-first delivery, monorepo#2059).
  // The site is a pure static build, so flags are baked at build time: flipping
  // one means a rebuild + redeploy, there is no runtime/per-user evaluation. Use
  // `astro:env` with a Zod-validated schema — type-safe over raw import.meta.env.
  // Convention + lifecycle (remove the gate once shipped) live in docs/README.md.
  env: {
    schema: {
      PUBLIC_CONTENT_PROFILE: envField.enum({
        context: "server", access: "public", values: ["default", "production-template"], default: "default",
      }),
      COMPANY_POSTAL_ADDRESS: envField.string({
        context: "server",
        access: "secret",
        optional: true,
      }),
      FEATURE_COMPANY_IDENTITY: envField.boolean({
        context: "server",
        access: "public",
        default: false,
      }),
      FEATURE_FEATURED_PORTFOLIO: envField.boolean({ context: "server", access: "public", default: true }),
      FEATURE_OFFER_COPY: envField.boolean({
        context: "server",
        access: "public",
        default: false,
      }),
      FEATURE_CLIENT_PORTFOLIO: envField.boolean({
        context: "server",
        access: "public",
        default: true,
      }),
      // Default-off preview notice on the rendered English and Danish homepages.
      // Default-off, so production builds omit it; a preview build enables it
      // with `FEATURE_PREVIEW_BANNER=true npm run build`. Server context = the
      // flag is read while the .astro component renders at build time (SSG), so
      // the banner's HTML is simply not emitted when off — no client JS needed.
      FEATURE_PREVIEW_BANNER: envField.boolean({
        context: "server",
        access: "public",
        default: false,
      }),
    },
  },
  integrations: [
    {
      name: 'devantler-business-pages',
      hooks: {
        'astro:config:setup': ({ injectRoute }) => {
          for (const pattern of ['/', '/da/', '/about/', '/da/about/', '/projects/', '/da/projects/', '/company/', '/da/company/', '/projects/active/', '/projects/completed/']) {
            injectRoute({ pattern, entrypoint: './src/components/business/BusinessPage.astro', prerender: true });
          }
        },
      },
    },
    mermaid(),
    starlight({
      title: "Devantler Tech",
      description:
        "Devantler Tech — a one-person software business building websites, apps and open-source tools.",
      components: {
        PageFrame: './src/components/business/JournalFrame.astro',
        PageTitle: './src/components/business/JournalTitle.astro',
        Head: './src/components/business/SupportingHead.astro',
        Header: './src/components/business/SupportingHeader.astro',
        Footer: './src/components/business/SupportingFooter.astro',
        ThemeSelect: './src/components/business/SupportingThemeSelect.astro',
      },
      defaultLocale: "en",
      logo: {
        src: "./src/assets/author.png",
        replacesTitle: false,
      },
      favicon: "/favicon.png",
      social: [
        {
          icon: "linkedin",
          label: "LinkedIn",
          href: "https://www.linkedin.com/in/nikolai-emil-damm-14a786150/",
        },
        {
          icon: "github",
          label: "GitHub",
          href: "https://github.com/devantler",
        },
        {
          icon: "rss",
          label: "RSS",
          href: "/blog/rss.xml",
        },
      ],
      editLink: {
        baseUrl:
          "https://github.com/devantler-tech/business-site/edit/main/",
      },
      customCss: ["./src/styles/custom.css", "./src/styles/business-docs.css"],
      plugins: [
        starlightBlog(journalConfig),
        starlightGithubAlerts(),
        starlightLinksValidator({
          errorOnRelativeLinks: false,
          exclude: ["/blog/**", "/blog/"],
        }),
      ],
      head: [
        {
          tag: "meta",
          attrs: { property: "og:image", content: "https://devantler.tech/author.png" },
        },
        {
          tag: "meta",
          attrs: { property: "og:type", content: "website" },
        },
        {
          tag: "meta",
          attrs: { name: "twitter:card", content: "summary_large_image" },
        },
        {
          tag: "meta",
          attrs: { name: "twitter:image", content: "https://devantler.tech/author.png" },
        },
        {
          tag: "meta",
          attrs: { name: "author", content: "Nikolai Emil Damm" },
        },
        {
          tag: "script",
          content: readFileSync(new URL('./src/components/business/journal-navigation.js', import.meta.url), 'utf8').trimEnd(),
        },
      ],
      sidebar: [
        {
          label: "About Devantler Tech",
          link: "/about/",
        },
        {
          label: "Agentic Engineering",
          link: "/agentic-engineering/",
        },
        {
          label: "Projects",
          link: "/projects/",
        },
        {
          label: "Templates",
          items: [{ autogenerate: { directory: "templates" } }],
        },
      ],
      lastUpdated: true,
      pagination: true,
      tableOfContents: { minHeadingLevel: 2, maxHeadingLevel: 3 },
    }),
    sitemap({ filter: page => new URL(page).pathname !== "/templates/gitops-tenant-template/" }),
  ],
});
