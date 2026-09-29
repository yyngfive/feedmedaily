// 内置邮件聚合源目录：手写静态文件，不并入会随 sci-rss-list 刷新被覆盖的
// feedCatalog.ts。条目只携带来源标识，绝不包含私有 RSS 地址；真实地址由
// 用户在设置里配置（SCIRSS_EMAIL_FEED_URL），订阅行落盘时只写 email_source。
export type EmailFeedCatalogEntry = {
  id: string;
  journal: string;
  publisher: string;
  description: string;
  configKey: string;
};

export const emailFeedCatalog: EmailFeedCatalogEntry[] = [
  {
    id: "rsc-email-alerts",
    journal: "RSC journals (email alerts)",
    publisher: "Royal Society of Chemistry",
    description:
      "Issue-alert emails from RSC, aggregated through a private kill-the-news feed. Each email expands into DOI-linked papers named after its own journal (e.g. Chemical Science, ChemComm).",
    configKey: "SCIRSS_EMAIL_FEED_URL",
  },
];
