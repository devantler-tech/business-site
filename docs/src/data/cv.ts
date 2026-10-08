// Single source of truth for the CV. The About page renders its header, meta cards and skills
// from it, the PDF endpoint renders all of it, and scripts/check-cv-drift.mjs keeps the page's
// hand-written experience roster in step with the roles below.

export interface Role {
  title: string;
  organisation: string;
  location: string;
  /** Employment form or seniority shown next to the organisation, e.g. "Consultant". */
  engagement?: string;
  period: string;
  summary?: string;
  highlights?: string[];
}

export interface Entry {
  title: string;
  subtitle?: string;
  period?: string;
  summary?: string;
  url?: string;
}

export interface SkillGroup {
  label: string;
  value: string;
}

export interface Link {
  label: string;
  url: string;
}

export interface Cv {
  /** Bump when the content changes: it is the "Updated" date printed on the PDF. */
  updated: string;
  name: string;
  title: string;
  location: string;
  website: Link;
  github: Link;
  linkedin: Link;
  profile: string;
  experience: Role[];
  earlierExperience: Role[];
  education: Entry[];
  talks: Entry[];
  openSource: Entry[];
  certifications: Entry[];
  courses: Entry[];
  community: Entry[];
  technicalSkills: SkillGroup[];
  personalSkills: SkillGroup[];
  languages: string;
  interests: string;
}

export const cv: Cv = {
  updated: "2026-09-15",
  name: "Nikolai Emil Damm",
  title: "Developer Experience Engineer",
  location: "Funen, Denmark",
  website: { label: "devantler.tech", url: "https://devantler.tech" },
  github: { label: "github.com/devantler", url: "https://github.com/devantler" },
  linkedin: {
    label: "linkedin.com/in/nikolai-emil-damm-14a786150",
    url: "https://www.linkedin.com/in/nikolai-emil-damm-14a786150/",
  },
  profile:
    "I am a Developer Experience Engineer based in Denmark with an MSc in Software Engineering. I build open-source developer tools, operate Kubernetes platforms, and care deeply about making engineers more effective. My focus is on CNCF technologies, GitOps, and reducing friction in the software delivery lifecycle.",

  experience: [
    {
      title: "Developer Experience Engineer",
      organisation: "TV2",
      location: "Odense",
      period: "Jun 2025 — Present",
      summary:
        "I improve the developer experience for TV2's product teams — working closely with them to find where they get stuck, and with other enabling teams to keep our tools aligned with the wider organization.",
      highlights: [
        "In May 2026 the team was renamed from Developer Tooling to Development Platform, as the remit grew past internal tooling to cover running and securing software too.",
      ],
    },
    {
      title: "Platform Engineer and Open Source Community Facilitator",
      organisation: "Energinet",
      location: "Fredericia",
      engagement: "Consultant",
      period: "Nov 2024 — Jun 2025",
      summary:
        "Promoted to Consultant and moved to the Platform team in the Infrastructure and Platforms department, where I helped scope, design, and implement a shared Kubernetes platform offering managed single- and multi-tenant clusters across Energinet.",
      highlights: [
        "Built and ran the Kubernetes platforms, tenancy models, self-service portals, GitOps tooling, and CI/CD pipelines.",
        "Operated and monitored on-prem servers.",
        "Grew an internal open-source community, hosting meetups, hackathons, and workshops.",
        "Championed GitHub Flow, DevOps, cloud-native, and open-source practices.",
      ],
    },
    {
      title: "Software Engineer and Open Source Community Facilitator",
      organisation: "Energinet",
      location: "Fredericia",
      engagement: "Junior Consultant",
      period: "Aug 2023 — Nov 2024",
      summary:
        "Part of the Substation Data team in the Innovation department, focused on platform engineering and IT/OT convergence.",
      highlights: [
        "Built and ran Kubernetes platforms, GitOps tooling, CI/CD pipelines, and IT/OT integrations.",
        "Operated and monitored on-prem servers.",
        "Grew an internal open-source community, hosting meetups, hackathons, and workshops.",
        "Championed GitHub Flow, DevOps, cloud-native, and open-source practices.",
      ],
    },
  ],

  earlierExperience: [
    {
      title: "Teaching Assistant",
      organisation: "University of Southern Denmark",
      location: "Odense",
      period: "Sep 2022 — Aug 2023",
      summary:
        "Teaching assistant in Big Data. Created course material, taught students, and helped them through their exercises.",
    },
    {
      title: "Student Software Developer",
      organisation: "Umbraco",
      location: "Odense",
      period: "Jul 2022 — Aug 2023",
      summary:
        "Part of the Umbraco Heartcore team, developing and maintaining the Umbraco Heartcore headless CMS — operated in Azure with Infrastructure as Code in Terraform and CI/CD pipelines in Azure DevOps, in a DevOps team focused on automation and monitoring.",
    },
    {
      title: "Student Software Developer",
      organisation: "FiftyTwo",
      location: "Kolding",
      period: "Nov 2021 — Jul 2022",
      summary:
        "Part of the development team for an e-commerce platform, working with .NET Framework, SVN, SQL Server and stored procedures, Kibana, and network monitoring.",
    },
    {
      title: "Student Software Developer",
      organisation: "Maersk Mc-Kinney Moller Institute",
      location: "Odense",
      period: "Feb 2021 — Nov 2021",
      summary:
        "Sole developer of a web platform used in research projects and course material to map and visualize Business Ecosystems with UML — C# and Blazor WebAssembly, SQL Server and Entity Framework, Azure and GitHub Actions — working closely with researchers and students.",
    },
    {
      title: "Teaching Assistant",
      organisation: "University of Southern Denmark",
      location: "Odense",
      period: "Sep 2020 — Dec 2020",
      summary: "Teaching assistant in Object-Oriented Programming.",
    },
    {
      title: "Student Software Developer",
      organisation: "GF Forsikring",
      location: "Odense",
      period: "Feb 2018 — May 2019",
      summary:
        "Part of the development team for the GF Forsikring website and landing pages — Sitecore, AngularJS, SQL Server, Git and SVN — working with the Marketing team to quickly create landing pages for campaigns.",
    },
  ],

  education: [
    {
      title: "MSc in Software Engineering",
      subtitle: "University of Southern Denmark, Odense",
      period: "2023",
      summary:
        "Thesis: “Exploration of State-of-the-Art Technology, Architectures and Tools to Create Future-Proof Data Spaces” — graded 12/12 (top grade).",
      url: "https://devantler.tech/pdfs/thesis.pdf",
    },
  ],

  talks: [
    {
      title: "KSail — a tool for creating, maintaining and operating Kubernetes clusters with ease",
      subtitle: "KCD Denmark 2024",
      summary:
        "KSail's public introduction — conceptual explanation and live demos of how it simplifies local development and CI workflows, and lets developers take on cluster operations earlier in the process.",
      url: "https://youtu.be/Q-Hfn_-B7p8",
    },
  ],

  openSource: [
    {
      title: "KSail",
      summary:
        "Bundles common Kubernetes tooling into a single binary. Create clusters, deploy workloads, and operate GitOps-based cloud-native stacks via CLI, VS Code extension, AI chat TUI, or MCP server.",
      url: "https://ksail.devantler.tech",
    },
    {
      title: "Platform",
      summary:
        "A Flux GitOps-based Kubernetes platform on Talos Linux and Hetzner Cloud — Cilium with the Gateway API, cert-manager, External Secrets with OpenBao, and external-dns.",
      url: "https://github.com/devantler-tech/platform",
    },
    {
      title: "Actions & Reusable Workflows",
      summary:
        "A curated library of GitHub Actions and reusable workflows for Go, .NET, docs, releases, and repository automation.",
      url: "https://github.com/devantler-tech/actions",
    },
  ],

  certifications: [
    { title: "GitHub Actions Certification", subtitle: "GitHub" },
    { title: "KCD Denmark 2024 Speaker", subtitle: "Credly badge" },
    { title: "Cilium: Discovery Platform Engineer", subtitle: "Credly badge" },
  ],

  courses: [
    { title: "Kubernetes for the Absolute Beginners", subtitle: "KodeKloud" },
    { title: "Kubernetes and Cloud-Native Associate (KCNA)", subtitle: "KodeKloud" },
  ],

  community: [
    {
      title: "VS Code & GitHub Copilot Backstage",
      subtitle: "Member",
      summary:
        "Early access to the latest offerings, testing pre-release features and giving feedback directly to the teams behind them.",
    },
  ],

  technicalSkills: [
    { label: "Cloud Native", value: "Kubernetes, GitOps (Flux), Cilium, CNCF ecosystem" },
    {
      label: "Platform & DevEx",
      value:
        "Self-service via CLI, declarative files, AI assistants, or ClickOps portals (Backstage, Port); multi-tenancy and golden paths",
    },
    { label: "Automation", value: "CI/CD, GitHub Actions, Terraform (IaC), Docker" },
    {
      label: "AI-Assisted Engineering",
      value:
        "Agentic coding assistants, AI-driven planning & implementation, and automating chores, fixes, and specified changes to focus on high-impact work",
    },
    { label: "Languages", value: "Go, YAML, TypeScript, Bash, C#/.NET, SQL" },
    { label: "Cloud & Operations", value: "Azure, AWS, on-prem operations and monitoring" },
  ],

  personalSkills: [
    { label: "Communication", value: "Public speaking, technical writing, Danish and English" },
    {
      label: "Collaboration",
      value: "Cross-team enablement, inner & open source collaboration, stakeholder engagement",
    },
    {
      label: "Leadership",
      value: "Driving initiatives, open-source community facilitation, mentoring and teaching",
    },
    {
      label: "Mindset",
      value: "Continuous learning, pragmatism, ownership, accountability, integrity, honesty, principled",
    },
  ],

  languages: "Danish, English",
  interests: "Fitness, running, gaming, music, technology",
};
