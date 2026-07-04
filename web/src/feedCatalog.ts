export type FeedCatalogEntry = {
  journal: string;
  publisher: string;
  url: string;
};

export const feedCatalog: FeedCatalogEntry[] = [
  {
    journal: "Nature",
    publisher: "Nature",
    url: "https://www.nature.com/nature.rss",
  },
  {
    journal: "Nature Biotechnology",
    publisher: "Nature",
    url: "https://www.nature.com/nbt.rss",
  },
  {
    journal: "Nature Methods",
    publisher: "Nature",
    url: "https://www.nature.com/nmeth.rss",
  },
  {
    journal: "Science",
    publisher: "Science",
    url: "https://www.science.org/action/showFeed?type=etoc&feed=rss&jc=science",
  },
  {
    journal: "Cell",
    publisher: "Cell",
    url: "https://www.cell.com/cell/current.rss",
  },
  {
    journal: "JACS",
    publisher: "ACS",
    url: "https://pubs.acs.org/action/showFeed?type=axatoc&feed=rss&jc=jacsat",
  },
  {
    journal: "Angewandte Chemie",
    publisher: "Wiley",
    url: "https://onlinelibrary.wiley.com/feed/15213773/most-recent",
  },
];
