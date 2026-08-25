-- 086_catalog_marketplace_links.sql
-- Bounded Catalog -> Marketplace lookup used by /settings/catalog.

CREATE INDEX IF NOT EXISTS marketplace_alias_catalog_links_idx
  ON marketplace_item_aliases(item_code, source, account_key, id)
  WHERE is_active = true;
