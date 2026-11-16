package legacymigration

type copySpec struct{ source, target, query, sourceFilter string }
type checkSpec struct{ name, query string }

// These are explicit mappings between the observed source contract and the
// current application. No provider call or credit/debit operation is replayed.
var copies = []copySpec{
	{"users", "farmers", `INSERT INTO {{target}}.farmers(id,full_name,phone,email,password_hash,role,location,lga,state,country,photo_url,bank_name,bank_code,account_name,account_number,wallet_number,is_admin,deleted_at,created_at,updated_at)
 SELECT m.target_id,u.full_name,u.phone,u.email,u.password_hash,u.role,
 COALESCE(NULLIF(u.location,''),c.name,l.name,s.name,''),COALESCE(NULLIF(u.lga,''),l.name),COALESCE(NULLIF(u.state,''),s.name),u.country,
 COALESCE(NULLIF(btrim(u.photo_url),''),'/static/assets/images/un.jpeg'),b.bank_name,b.bank_code,b.account_name,b.account_number,
 wn.wallet_number,u.is_admin,u.deleted_at,u.created_at,u.updated_at
 FROM {{source}}.users u JOIN {{target}}.legacy_import_mappings m ON m.source_table='users' AND m.source_id=u.id
 LEFT JOIN {{source}}.states s ON s.id=u.state_id LEFT JOIN {{source}}.lgas l ON l.id=u.lga_id
 LEFT JOIN {{source}}.communities c ON c.id=u.community_id LEFT JOIN {{source}}.user_bank_accounts b ON b.user_id=u.id
 JOIN {{target}}.legacy_wallet_numbers wn ON wn.source_user_id=u.id AND wn.target_user_id=m.target_id`, ""},
	{"products", "crops", `INSERT INTO {{target}}.crops(id,farmer_id,name,quantity,unit,price_per_unit,location,lga,state,country,latitude,longitude,location_accuracy,listed_for_sale,image_url,initial_listed_quantity,first_listed_at,first_order_at,sold_out_at,created_at,updated_at)
 SELECT pmap.target_id,umap.target_id,p.name,p.quantity,p.unit,p.price,
 COALESCE(c.name,l.name,s.name,NULLIF(u.location,''),''),COALESCE(l.name,u.lga),COALESCE(s.name,u.state),u.country,
 p.latitude,p.longitude,p.location_accuracy,p.status='active',
 COALESCE((SELECT i.image_url FROM {{source}}.product_images i WHERE i.product_id=p.id ORDER BY i.is_primary DESC,i.sort_order,i.id LIMIT 1),'/static/assets/images/un.jpeg'),
 p.initial_listed_quantity,p.first_listed_at,p.first_order_at,p.sold_out_at,p.created_at,p.updated_at
 FROM {{source}}.products p JOIN {{target}}.legacy_import_mappings pmap ON pmap.source_table='products' AND pmap.source_id=p.id
 JOIN {{target}}.legacy_import_mappings umap ON umap.source_table='users' AND umap.source_id=p.owner_id
 JOIN {{source}}.users u ON u.id=p.owner_id LEFT JOIN {{source}}.states s ON s.id=p.state_id
 LEFT JOIN {{source}}.lgas l ON l.id=p.lga_id LEFT JOIN {{source}}.communities c ON c.id=p.community_id`, ""},
	{"product_images", "crop_images", `INSERT INTO {{target}}.crop_images(id,crop_id,image_url,is_primary,sort_order,created_at)
 SELECT i.id,m.target_id,i.image_url,i.is_primary,i.sort_order,i.created_at FROM {{source}}.product_images i
 JOIN {{target}}.legacy_import_mappings m ON m.source_table='products' AND m.source_id=i.product_id`, ""},
	{"product_price_history", "produce_price_history", `INSERT INTO {{target}}.produce_price_history(id,crop_id,price_per_unit,unit,source,observed_at)
 SELECT h.id,m.target_id,h.price,h.unit,h.source,h.recorded_at FROM {{source}}.product_price_history h
 JOIN {{target}}.legacy_import_mappings m ON m.source_table='products' AND m.source_id=h.product_id`, ""},
	{"inventory_history", "inventory_history", `INSERT INTO {{target}}.inventory_history(id,crop_id,quantity_before,quantity_change,quantity_after,reason,created_at)
 SELECT h.id,m.target_id,h.quantity_before,h.quantity_change,h.quantity_after,h.reason,h.created_at FROM {{source}}.inventory_history h
 JOIN {{target}}.legacy_import_mappings m ON m.source_table='products' AND m.source_id=h.product_id`, ""},
	{"wallets", "wallets", `INSERT INTO {{target}}.wallets(id,farmer_id,balance,currency,created_at,updated_at)
 SELECT wm.target_id,um.target_id,w.balance,w.currency,w.created_at,w.updated_at FROM {{source}}.wallets w
 JOIN {{target}}.legacy_import_mappings wm ON wm.source_table='wallets' AND wm.source_id=w.id
 JOIN {{target}}.legacy_import_mappings um ON um.source_table='users' AND um.source_id=w.owner_id`, ""},
	{"orders", "orders", `INSERT INTO {{target}}.orders(id,buyer_id,crop_id,quantity,total_price,status,created_at,checkout_token)
 SELECT om.target_id,um.target_id,pm.target_id,i.quantity,o.total_amount,o.status,o.created_at,o.checkout_key
 FROM {{source}}.orders o JOIN {{source}}.order_items i ON i.order_id=o.id
 JOIN {{target}}.legacy_import_mappings om ON om.source_table='orders' AND om.source_id=o.id
 JOIN {{target}}.legacy_import_mappings um ON um.source_table='users' AND um.source_id=o.buyer_id
 JOIN {{target}}.legacy_import_mappings pm ON pm.source_table='products' AND pm.source_id=i.product_id`, ""},
	{"payments", "payments", `INSERT INTO {{target}}.payments(id,order_id,reference,transaction_id,amount,currency,status,provider,created_at,updated_at)
 SELECT p.id,om.target_id,p.reference,p.transaction_id,p.amount,p.currency,p.status,p.provider,p.created_at,p.updated_at
 FROM {{source}}.payments p JOIN {{target}}.legacy_import_mappings om ON om.source_table='orders' AND om.source_id=p.order_id`, ""},
	{"deliveries", "deliveries", `INSERT INTO {{target}}.deliveries(id,order_id,buyer_id,farmer_id,crop_id,quantity,delivery_address,lga,state,country,status,tracking_number,picked_up_at,delivered_at,created_at,updated_at)
 SELECT d.id,om.target_id,um.target_id,sm.target_id,pm.target_id,i.quantity,d.destination_address,l.name,s.name,u.country,d.status,d.tracking_code,d.picked_up_at,d.delivered_at,d.created_at,d.updated_at
 FROM {{source}}.deliveries d JOIN {{source}}.orders o ON o.id=d.order_id JOIN {{source}}.order_items i ON i.order_id=o.id
 JOIN {{source}}.products p ON p.id=i.product_id JOIN {{source}}.users u ON u.id=o.buyer_id
 JOIN {{target}}.legacy_import_mappings om ON om.source_table='orders' AND om.source_id=o.id
 JOIN {{target}}.legacy_import_mappings um ON um.source_table='users' AND um.source_id=o.buyer_id
 JOIN {{target}}.legacy_import_mappings sm ON sm.source_table='users' AND sm.source_id=p.owner_id
 JOIN {{target}}.legacy_import_mappings pm ON pm.source_table='products' AND pm.source_id=p.id
 LEFT JOIN {{source}}.lgas l ON l.id=d.destination_lga_id LEFT JOIN {{source}}.states s ON s.id=d.destination_state_id`, ""},
	{"wallet_transactions", "wallet_transactions", `INSERT INTO {{target}}.wallet_transactions(id,wallet_id,type,amount,reference,created_at)
 SELECT t.id,wm.target_id,t.type,t.amount,t.reference,t.created_at FROM {{source}}.wallet_transactions t
 JOIN {{target}}.legacy_import_mappings wm ON wm.source_table='wallets' AND wm.source_id=t.wallet_id`, ""},
	{"wallet_deposits", "wallet_deposits", `INSERT INTO {{target}}.wallet_deposits(id,wallet_id,provider,reference,provider_transaction_id,amount,currency,verified_amount,verified_currency,status,failure_reason,created_at,updated_at)
 SELECT d.id,wm.target_id,d.provider,d.reference,d.provider_transaction_id,d.amount,d.currency,d.verified_amount,d.verified_currency,d.status,d.failure_reason,d.created_at,d.updated_at
 FROM {{source}}.wallet_deposits d JOIN {{target}}.legacy_import_mappings wm ON wm.source_table='wallets' AND wm.source_id=d.wallet_id`, ""},
	{"wallet_withdrawals", "wallet_withdrawals", `INSERT INTO {{target}}.wallet_withdrawals(id,wallet_id,farmer_id,reference,provider_transfer_id,amount,currency,bank_name,bank_code,account_name,account_number,status,approved_by,reminded_at,name_check_status,name_check_resolved,name_check_detail,name_check_at,created_at,updated_at)
 SELECT w.id,wm.target_id,um.target_id,w.reference,w.provider_transfer_id,w.amount,w.currency,w.bank_name,w.bank_code,w.account_name,w.account_number,w.status,am.target_id,w.reminded_at,w.name_check_status,w.name_check_resolved,w.name_check_detail,w.name_check_at,w.created_at,w.updated_at
 FROM {{source}}.wallet_withdrawals w JOIN {{target}}.legacy_import_mappings wm ON wm.source_table='wallets' AND wm.source_id=w.wallet_id
 JOIN {{target}}.legacy_import_mappings um ON um.source_table='users' AND um.source_id=w.user_id
 LEFT JOIN {{target}}.legacy_import_mappings am ON am.source_table='users' AND am.source_id=w.approved_by`, ""},
	{"wallet_transfers", "wallet_transfers", `INSERT INTO {{target}}.wallet_transfers(id,sender_wallet_id,sender_id,recipient_wallet_id,recipient_id,recipient_phone,reference,amount,currency,note,status,approved_by,reminded_at,created_at,updated_at)
 SELECT t.id,sw.target_id,su.target_id,rw.target_id,ru.target_id,t.recipient_phone,t.reference,t.amount,t.currency,t.note,t.status,am.target_id,t.reminded_at,t.created_at,t.updated_at
 FROM {{source}}.wallet_transfers t
 JOIN {{target}}.legacy_import_mappings sw ON sw.source_table='wallets' AND sw.source_id=t.sender_wallet_id
 JOIN {{target}}.legacy_import_mappings su ON su.source_table='users' AND su.source_id=t.sender_id
 JOIN {{target}}.legacy_import_mappings rw ON rw.source_table='wallets' AND rw.source_id=t.recipient_wallet_id
 JOIN {{target}}.legacy_import_mappings ru ON ru.source_table='users' AND ru.source_id=t.recipient_id
 LEFT JOIN {{target}}.legacy_import_mappings am ON am.source_table='users' AND am.source_id=t.approved_by`, ""},
	{"wallet_virtual_accounts", "wallet_virtual_accounts", `INSERT INTO {{target}}.wallet_virtual_accounts(id,farmer_id,provider,tx_ref,order_ref,provider_ref,account_number,bank_name,account_name,currency,status,identity_kind,consent_version,consent_at,failure_reason,last_checked_at,created_at,updated_at)
 SELECT v.id,um.target_id,v.provider,v.tx_ref,v.order_ref,v.provider_ref,v.account_number,v.bank_name,v.account_name,v.currency,v.status,v.identity_kind,v.consent_version,v.consent_at,v.failure_reason,v.last_checked_at,v.created_at,v.updated_at
 FROM {{source}}.wallet_virtual_accounts v JOIN {{target}}.legacy_import_mappings um ON um.source_table='users' AND um.source_id=v.user_id`, ""},
	{"platform_revenue", "platform_revenue", `INSERT INTO {{target}}.platform_revenue(id,order_id,reference,gross_amount,amount,currency,rate_percent,created_at)
 SELECT p.id,om.target_id,p.reference,p.gross_amount,p.amount,p.currency,p.rate_percent,p.created_at
 FROM {{source}}.platform_revenue p JOIN {{target}}.legacy_import_mappings om ON om.source_table='orders' AND om.source_id=p.order_id`, ""},
	{"webhook_events", "webhook_events", `INSERT INTO {{target}}.webhook_events(id,transaction_id,event,payload,processed_at,created_at)
 SELECT id,transaction_id,event,payload,processed_at,created_at FROM {{source}}.webhook_events`, ""},
	{"negotiations", "negotiations", `INSERT INTO {{target}}.negotiations(id,crop_id,buyer_id,farmer_id,quantity,initial_offer_price,current_offer_price,round_count,max_rounds,status,expires_at,accepted_at,closed_at,created_at,updated_at)
 SELECT n.id,pm.target_id,um.target_id,sm.target_id,n.quantity,n.initial_offer_price,n.current_offer_price,n.round_count,n.max_rounds,n.status,n.expires_at,n.accepted_at,n.closed_at,n.created_at,n.updated_at
 FROM {{source}}.negotiations n JOIN {{source}}.products p ON p.id=n.product_id
 JOIN {{target}}.legacy_import_mappings pm ON pm.source_table='products' AND pm.source_id=n.product_id
 JOIN {{target}}.legacy_import_mappings um ON um.source_table='users' AND um.source_id=n.buyer_id
 JOIN {{target}}.legacy_import_mappings sm ON sm.source_table='users' AND sm.source_id=p.owner_id`, ""},
	{"negotiation_messages", "negotiation_messages", `INSERT INTO {{target}}.negotiation_messages(id,negotiation_id,sender_id,message_type,message,offer_price,offer_status,created_at)
 SELECT n.id,n.negotiation_id,um.target_id,n.message_type,COALESCE(n.message,''),COALESCE(n.offer_price,0),n.offer_status,n.created_at
 FROM {{source}}.negotiation_messages n JOIN {{target}}.legacy_import_mappings um ON um.source_table='users' AND um.source_id=n.sender_id`, ""},
	{"cart_items", "cart_items", `INSERT INTO {{target}}.cart_items(id,buyer_id,crop_id,negotiation_id,quantity,price_per_unit,total_price,created_at)
 SELECT c.id,um.target_id,pm.target_id,c.negotiation_id,c.quantity,c.price_per_unit,c.total_price,c.created_at
 FROM {{source}}.cart_items c JOIN {{target}}.legacy_import_mappings um ON um.source_table='users' AND um.source_id=c.buyer_id
 JOIN {{target}}.legacy_import_mappings pm ON pm.source_table='products' AND pm.source_id=c.product_id`, ""},
	{"market_events", "market_events", `INSERT INTO {{target}}.market_events(id,event_type,crop_id,user_id,session_id,metadata,created_at)
 SELECT e.id,e.event_type,pm.target_id,um.target_id,e.session_id,e.metadata,e.created_at FROM {{source}}.market_events e
 LEFT JOIN {{target}}.legacy_import_mappings pm ON pm.source_table='products' AND pm.source_id=e.product_id
 LEFT JOIN {{target}}.legacy_import_mappings um ON um.source_table='users' AND um.source_id=e.user_id`, ""},
	{"marketplace_searches", "marketplace_searches", `INSERT INTO {{target}}.marketplace_searches(id,search_term,normalized_produce,location,user_id,session_id,created_at)
 SELECT s.id,s.search_term,s.normalized_produce,s.location,um.target_id,s.session_id,s.created_at FROM {{source}}.marketplace_searches s
 LEFT JOIN {{target}}.legacy_import_mappings um ON um.source_table='users' AND um.source_id=s.user_id`, ""},
	{"external_market_prices", "external_market_prices", `INSERT INTO {{target}}.external_market_prices(id,produce_type,price_per_unit,unit,currency,provider,market_name,lga,state,country,collected_at,imported_at,source_url)
 SELECT id,produce_type,price_per_unit,unit,currency,provider,market_name,lga,state,country,collected_at,imported_at,source_url FROM {{source}}.external_market_prices`, ""},
	{"lga_neighbors", "lga_neighbors", `INSERT INTO {{target}}.lga_neighbors(state,lga,neighbor_lga)
 SELECT s.name,l.name,n.name FROM {{source}}.lga_neighbors e JOIN {{source}}.lgas l ON l.id=e.lga_id JOIN {{source}}.states s ON s.id=l.state_id JOIN {{source}}.lgas n ON n.id=e.neighbor_lga_id`, ""},
	{"diagnoses", "diagnoses", `INSERT INTO {{target}}.diagnoses(id,farmer_id,category,description,result,image_name,created_at)
 SELECT d.id,um.target_id,COALESCE(d.category,''),COALESCE(d.symptoms,''),COALESCE(d.result,''),
 (SELECT COALESCE(NULLIF(m.image_name,''),m.media_url) FROM {{source}}.diagnosis_media m WHERE m.diagnosis_id=d.id ORDER BY m.id LIMIT 1),d.created_at
 FROM {{source}}.diagnoses d JOIN {{target}}.legacy_import_mappings um ON um.source_table='users' AND um.source_id=d.user_id`, ""},
	{"diagnosis_media", "diagnosis_media", `INSERT INTO {{target}}.diagnosis_media(id,diagnosis_id,media_type,file_name,created_at)
 SELECT id,diagnosis_id,media_type,COALESCE(NULLIF(image_name,''),media_url),created_at FROM {{source}}.diagnosis_media`, ""},
	{"notifications", "notifications", `INSERT INTO {{target}}.notifications(id,user_id,title,message,type,is_read,created_at)
 SELECT n.id,um.target_id,n.title,n.message,n.notification_type,n.is_read,n.created_at FROM {{source}}.notifications n
 JOIN {{target}}.legacy_import_mappings um ON um.source_table='users' AND um.source_id=n.user_id`, ""},
	{"conversations", "conversations", `INSERT INTO {{target}}.conversations(id,name,type,created_by,created_at,updated_at)
 SELECT c.id,c.title,c.conversation_type,um.target_id,c.created_at,c.updated_at FROM {{source}}.conversations c
 JOIN {{target}}.legacy_import_mappings um ON um.source_table='users' AND um.source_id=c.created_by`, ""},
	{"conversation_members", "conversation_members", `INSERT INTO {{target}}.conversation_members(id,conversation_id,user_id,joined_at,last_read_at)
 SELECT c.id,c.conversation_id,um.target_id,c.joined_at,c.last_read_at FROM {{source}}.conversation_members c
 JOIN {{target}}.legacy_import_mappings um ON um.source_table='users' AND um.source_id=c.user_id WHERE c.left_at IS NULL`, "WHERE left_at IS NULL"},
	{"chat_messages", "chat_messages", `INSERT INTO {{target}}.chat_messages(id,conversation_id,sender_id,message,created_at)
 SELECT c.id,c.conversation_id,um.target_id,COALESCE(c.message,''),c.created_at FROM {{source}}.chat_messages c
 JOIN {{target}}.legacy_import_mappings um ON um.source_table='users' AND um.source_id=c.sender_id`, ""},
}

var identityTables = []string{"farmers", "crops", "crop_images", "produce_price_history", "inventory_history", "wallets", "orders", "payments", "deliveries", "wallet_transactions", "wallet_deposits", "wallet_withdrawals", "wallet_transfers", "wallet_virtual_accounts", "platform_revenue", "webhook_events", "negotiations", "negotiation_messages", "cart_items", "market_events", "marketplace_searches", "external_market_prices", "diagnoses", "diagnosis_media", "notifications", "conversations", "conversation_members", "chat_messages"}

var inventoryTables = []string{"users", "products", "wallets", "orders", "order_items", "payments", "states", "lgas", "communities", "user_bank_accounts", "product_images", "product_price_history", "inventory_history", "market_events", "marketplace_searches", "external_market_prices", "lga_neighbors", "negotiations", "negotiation_messages", "cart_items", "deliveries", "delivery_tracking", "wallet_transactions", "wallet_deposits", "wallet_withdrawals", "wallet_transfers", "wallet_virtual_accounts", "platform_revenue", "webhook_events", "notifications", "diagnoses", "diagnosis_media", "conversations", "conversation_members", "chat_messages", "user_sessions", "otp_codes", "user_verifications", "organizations", "organization_members", "product_status_history", "product_favorites", "product_reviews", "user_reviews"}

var requiredColumns = map[string][]string{
	"users":       {"id", "full_name", "phone", "email", "password_hash", "role", "location", "lga", "state", "country", "photo_url", "is_admin", "deleted_at", "state_id", "lga_id", "community_id", "created_at", "updated_at"},
	"products":    {"id", "owner_type", "owner_id", "name", "quantity", "unit", "price", "status", "state_id", "lga_id", "community_id", "latitude", "longitude", "location_accuracy", "initial_listed_quantity", "first_listed_at", "first_order_at", "sold_out_at", "created_at", "updated_at"},
	"wallets":     {"id", "owner_type", "owner_id", "balance", "currency", "wallet_number", "created_at", "updated_at"},
	"orders":      {"id", "buyer_id", "status", "total_amount", "created_at", "checkout_key", "currency"},
	"payments":    {"id", "order_id", "reference", "transaction_id", "amount", "currency", "status", "provider", "created_at", "updated_at"},
	"order_items": {"id", "order_id", "product_id", "quantity", "unit_price", "total_price"},
	"states":      {"id", "name"}, "lgas": {"id", "state_id", "name"}, "communities": {"id", "lga_id", "name"},
	"user_bank_accounts":      {"user_id", "bank_name", "bank_code", "account_name", "account_number"},
	"product_images":          {"id", "product_id", "image_url", "is_primary", "sort_order", "created_at"},
	"product_price_history":   {"id", "product_id", "price", "unit", "source", "recorded_at"},
	"inventory_history":       {"id", "product_id", "quantity_before", "quantity_change", "quantity_after", "reason", "created_at"},
	"wallet_transactions":     {"id", "wallet_id", "type", "amount", "reference", "created_at"},
	"wallet_deposits":         {"id", "wallet_id", "provider", "reference", "provider_transaction_id", "amount", "currency", "verified_amount", "verified_currency", "status", "failure_reason", "created_at", "updated_at"},
	"wallet_withdrawals":      {"id", "wallet_id", "user_id", "reference", "provider_transfer_id", "amount", "currency", "bank_name", "bank_code", "account_name", "account_number", "status", "approved_by", "reminded_at", "name_check_status", "name_check_resolved", "name_check_detail", "name_check_at", "created_at", "updated_at"},
	"wallet_transfers":        {"id", "sender_wallet_id", "sender_id", "recipient_wallet_id", "recipient_id", "recipient_phone", "reference", "amount", "currency", "note", "status", "approved_by", "reminded_at", "created_at", "updated_at"},
	"wallet_virtual_accounts": {"id", "user_id", "provider", "tx_ref", "order_ref", "provider_ref", "account_number", "bank_name", "account_name", "currency", "status", "identity_kind", "consent_version", "consent_at", "failure_reason", "last_checked_at", "created_at", "updated_at"},
	"market_events":           {"id", "event_type", "product_id", "user_id", "session_id", "metadata", "created_at"},
	"negotiations":            {"id", "product_id", "buyer_id", "quantity", "initial_offer_price", "current_offer_price", "round_count", "max_rounds", "status", "expires_at", "accepted_at", "closed_at", "created_at", "updated_at"},
	"negotiation_messages":    {"id", "negotiation_id", "sender_id", "message_type", "message", "offer_price", "offer_status", "created_at"},
	"cart_items":              {"id", "buyer_id", "product_id", "negotiation_id", "quantity", "price_per_unit", "total_price", "created_at"},
	"diagnoses":               {"id", "user_id", "category", "symptoms", "result", "created_at"},
	"diagnosis_media":         {"id", "diagnosis_id", "media_type", "media_url", "image_name", "created_at"},
	"conversations":           {"id", "title", "conversation_type", "created_by", "created_at", "updated_at"},
	"conversation_members":    {"id", "conversation_id", "user_id", "joined_at", "last_read_at", "left_at"},
	"chat_messages":           {"id", "conversation_id", "sender_id", "message", "media_url", "created_at"},
}
