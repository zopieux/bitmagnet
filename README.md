# bitmagnet

A self-hosted BitTorrent indexer, DHT crawler, content classifier and torrent search engine with web UI, GraphQL API and Servarr stack integration.

This fork:
* switches to sqlite to avoid the postgres dependency
* switches to SolidJS
* optimizes (batches) for slow storage mediums
* simplifies the classification and defers it to an API endpoint
