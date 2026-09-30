-- First availability is a separate immutable receipt recorded after the target
-- store acknowledges success. Existing processed_at is only a historical,
-- conservative processing-stage reconstruction, never an exact first receipt.
CREATE TABLE IF NOT EXISTS measurement_availability (
 tenant_id text NOT NULL, message_id text NOT NULL, source text NOT NULL,
 available_at bigint NOT NULL, recorded_at bigint NOT NULL,
 PRIMARY KEY(tenant_id,message_id,source)
);
CREATE TABLE IF NOT EXISTS analytics_source_collection (
 source text PRIMARY KEY,collection_started_at bigint NOT NULL,
 backfill_start bigint,backfill_end bigint,backfill_status text NOT NULL DEFAULT 'NOT_STARTED',
 historical_quality text NOT NULL DEFAULT 'UNKNOWN'
);
INSERT INTO analytics_source_collection(source,collection_started_at,backfill_status,historical_quality)
 SELECT v,(extract(epoch FROM clock_timestamp())*1000)::bigint,'NOT_STARTED','PARTIAL'
 FROM unnest(ARRAY['standard_message','raw_archive_index','duty_business_event','device_state_event','duty_record','configuration_history']) v
 ON CONFLICT(source) DO NOTHING;
CREATE TABLE IF NOT EXISTS analytics_configuration_event (
 seq bigserial PRIMARY KEY,tenant_id text NOT NULL,source text NOT NULL,resource_id text NOT NULL,
 resource_version bigint NOT NULL,device_id text NOT NULL DEFAULT '',product_id text NOT NULL DEFAULT '',
 occurred_at bigint NOT NULL,recorded_at bigint NOT NULL,body jsonb NOT NULL,initial_snapshot boolean NOT NULL DEFAULT false,
 UNIQUE(tenant_id,source,resource_id,resource_version)
);
CREATE INDEX IF NOT EXISTS analytics_configuration_time_idx ON analytics_configuration_event(tenant_id,source,occurred_at,seq);
CREATE INDEX IF NOT EXISTS analytics_configuration_device_idx ON analytics_configuration_event(tenant_id,device_id,occurred_at,seq);
CREATE OR REPLACE FUNCTION analytics_configuration_projection(source_name text,payload jsonb) RETURNS jsonb AS $$
 SELECT CASE source_name
 WHEN 'iot_product' THEN jsonb_strip_nulls(jsonb_build_object('id',payload->'id','productId',payload->'id','name',payload->'name','category',payload->'category','status',payload->'status','thingModel',payload->'thingModel','protocolPackageId',payload->'protocolPackageId','transport',payload->'transport','payloadFormat',payload->'payloadFormat'))
 WHEN 'alarm_rule' THEN jsonb_strip_nulls(jsonb_build_object('id',payload->'id','productId',payload->'productId','name',payload->'name','alarmType',payload->'alarmType','level',payload->'level','conditions',payload->'conditions','match',payload->'match','durationSeconds',payload->'durationSeconds','recovery',payload->'recovery','expression',payload->'expression','enabled',payload->'enabled','version',payload->'version','actions',COALESCE((SELECT jsonb_agg(jsonb_build_object('type',a->'type')) FROM jsonb_array_elements(CASE WHEN jsonb_typeof(payload->'actions')='array' THEN payload->'actions' ELSE '[]'::jsonb END)a),'[]'::jsonb)))
 ELSE '{}'::jsonb END
$$ LANGUAGE SQL IMMUTABLE;
CREATE OR REPLACE FUNCTION analytics_record_configuration() RETURNS trigger AS $$
DECLARE at_ms bigint; revision bigint; snapshot jsonb; device text; product text;
BEGIN
 at_ms := (extract(epoch FROM clock_timestamp())*1000)::bigint;
 -- Serialize each resource independently. A sequence allocation alone is not
 -- used as a committed watermark anywhere in the read contract.
 PERFORM pg_advisory_xact_lock(hashtextextended(NEW.tenant_id||':'||TG_TABLE_NAME||':'||NEW.id,83491055));
 SELECT COALESCE(max(resource_version),0)+1 INTO revision FROM analytics_configuration_event WHERE tenant_id=NEW.tenant_id AND source=TG_TABLE_NAME AND resource_id=NEW.id;
 snapshot:=analytics_configuration_projection(TG_TABLE_NAME,NEW.body);
 device:=''; product:='';
 IF TG_TABLE_NAME='device_registry' THEN
  device:=NEW.id; product:=NEW.product_id;
  snapshot:=jsonb_build_object('gatewayId',NEW.body->>'gatewayId','connectorProfileId',NEW.body->>'connectorProfileId','collectorId',NEW.body->>'collectorId','productId',NEW.product_id);
 ELSIF TG_TABLE_NAME='iot_product' THEN
  product:=NEW.id;
 ELSIF TG_TABLE_NAME='alarm_rule' THEN
  product:=COALESCE(NEW.product_id,'');
 END IF;
 INSERT INTO analytics_configuration_event(tenant_id,source,resource_id,resource_version,device_id,product_id,occurred_at,recorded_at,body) VALUES(NEW.tenant_id,TG_TABLE_NAME,NEW.id,revision,device,product,at_ms,at_ms,snapshot);
 RETURN NEW;
END $$ LANGUAGE plpgsql;
DROP TRIGGER IF EXISTS analytics_device_configuration ON device_registry;
CREATE TRIGGER analytics_device_configuration AFTER INSERT OR UPDATE OF body ON device_registry FOR EACH ROW EXECUTE FUNCTION analytics_record_configuration();
DROP TRIGGER IF EXISTS analytics_product_configuration ON iot_product;
CREATE TRIGGER analytics_product_configuration AFTER INSERT OR UPDATE OF body ON iot_product FOR EACH ROW EXECUTE FUNCTION analytics_record_configuration();
DROP TRIGGER IF EXISTS analytics_rule_configuration ON alarm_rule;
CREATE TRIGGER analytics_rule_configuration AFTER INSERT OR UPDATE OF body ON alarm_rule FOR EACH ROW EXECUTE FUNCTION analytics_record_configuration();
-- Baselines begin at deployment. They intentionally do not reconstruct any
-- effective relation or rule version before collection started.
INSERT INTO analytics_configuration_event(tenant_id,source,resource_id,resource_version,device_id,product_id,occurred_at,recorded_at,body,initial_snapshot)
 SELECT tenant_id,'device_registry',id,1,id,product_id,c.collection_started_at,c.collection_started_at,jsonb_build_object('gatewayId',body->>'gatewayId','connectorProfileId',body->>'connectorProfileId','collectorId',body->>'collectorId','productId',product_id),true FROM device_registry CROSS JOIN analytics_source_collection c WHERE c.source='configuration_history'
 ON CONFLICT DO NOTHING;
INSERT INTO analytics_configuration_event(tenant_id,source,resource_id,resource_version,product_id,occurred_at,recorded_at,body,initial_snapshot)
 SELECT tenant_id,'iot_product',id,1,id,c.collection_started_at,c.collection_started_at,analytics_configuration_projection('iot_product',body),true FROM iot_product CROSS JOIN analytics_source_collection c WHERE c.source='configuration_history'
 ON CONFLICT DO NOTHING;
INSERT INTO analytics_configuration_event(tenant_id,source,resource_id,resource_version,product_id,occurred_at,recorded_at,body,initial_snapshot)
 SELECT tenant_id,'alarm_rule',id,1,COALESCE(product_id,''),c.collection_started_at,c.collection_started_at,analytics_configuration_projection('alarm_rule',body),true FROM alarm_rule CROSS JOIN analytics_source_collection c WHERE c.source='configuration_history'
 ON CONFLICT DO NOTHING;
-- Raw reservations retain immutable routing metadata even when payloads are in
-- ClickHouse. Only successfully archived ingress observations start a collector
-- relation. No inference from a device name or current group membership occurs.
CREATE OR REPLACE FUNCTION analytics_record_collector() RETURNS trigger AS $$
DECLARE collector text; previous text; revision bigint; at_ms bigint; started bigint;
BEGIN
 SELECT metadata->>'collectorId' INTO collector FROM raw_ingest_reservation WHERE tenant_id=NEW.tenant_id AND message_id=NEW.message_id;
 IF COALESCE(collector,'')='' THEN RETURN NEW; END IF;
 PERFORM pg_advisory_xact_lock(hashtextextended(NEW.tenant_id||':raw_collector:'||NEW.device_id,83491055));
 SELECT body->>'collectorId' INTO previous FROM analytics_configuration_event WHERE tenant_id=NEW.tenant_id AND source='raw_collector' AND resource_id=NEW.device_id ORDER BY occurred_at DESC,resource_version DESC LIMIT 1;
 IF previous=collector THEN RETURN NEW; END IF;
 SELECT collection_started_at INTO started FROM analytics_source_collection WHERE source='configuration_history';
 at_ms:=GREATEST(NEW.received_at,started);
 SELECT COALESCE(max(resource_version),0)+1 INTO revision FROM analytics_configuration_event WHERE tenant_id=NEW.tenant_id AND source='raw_collector' AND resource_id=NEW.device_id;
 INSERT INTO analytics_configuration_event(tenant_id,source,resource_id,resource_version,device_id,product_id,occurred_at,recorded_at,body) VALUES(NEW.tenant_id,'raw_collector',NEW.device_id,revision,NEW.device_id,NEW.product_id,at_ms,(extract(epoch FROM clock_timestamp())*1000)::bigint,jsonb_build_object('collectorId',collector,'rawMessageId',NEW.message_id));
 RETURN NEW;
END $$ LANGUAGE plpgsql;
DROP TRIGGER IF EXISTS analytics_collector_observation ON raw_archive_index;
CREATE TRIGGER analytics_collector_observation AFTER INSERT ON raw_archive_index FOR EACH ROW EXECUTE FUNCTION analytics_record_collector();
-- Deletion closes the known relation interval; retaining the old membership
-- after its inventory record disappeared would overstate dependencies.
CREATE OR REPLACE FUNCTION analytics_record_configuration_delete() RETURNS trigger AS $$
DECLARE at_ms bigint; revision bigint; device text; product text;
BEGIN
 at_ms := (extract(epoch FROM clock_timestamp())*1000)::bigint;
 PERFORM pg_advisory_xact_lock(hashtextextended(OLD.tenant_id||':'||TG_TABLE_NAME||':'||OLD.id,83491055));
 SELECT COALESCE(max(resource_version),0)+1 INTO revision FROM analytics_configuration_event WHERE tenant_id=OLD.tenant_id AND source=TG_TABLE_NAME AND resource_id=OLD.id;
 device:='';product:='';
 IF TG_TABLE_NAME='device_registry' THEN device:=OLD.id;product:=OLD.product_id;
 ELSIF TG_TABLE_NAME='iot_product' THEN product:=OLD.id;
 ELSIF TG_TABLE_NAME='alarm_rule' THEN product:=COALESCE(OLD.product_id,''); END IF;
 INSERT INTO analytics_configuration_event(tenant_id,source,resource_id,resource_version,device_id,product_id,occurred_at,recorded_at,body) VALUES(OLD.tenant_id,TG_TABLE_NAME,OLD.id,revision,device,product,at_ms,at_ms,jsonb_build_object('deleted',true));
 RETURN OLD;
END $$ LANGUAGE plpgsql;
DROP TRIGGER IF EXISTS analytics_device_configuration_delete ON device_registry;
CREATE TRIGGER analytics_device_configuration_delete AFTER DELETE ON device_registry FOR EACH ROW EXECUTE FUNCTION analytics_record_configuration_delete();
DROP TRIGGER IF EXISTS analytics_product_configuration_delete ON iot_product;
CREATE TRIGGER analytics_product_configuration_delete AFTER DELETE ON iot_product FOR EACH ROW EXECUTE FUNCTION analytics_record_configuration_delete();
DROP TRIGGER IF EXISTS analytics_rule_configuration_delete ON alarm_rule;
CREATE TRIGGER analytics_rule_configuration_delete AFTER DELETE ON alarm_rule FOR EACH ROW EXECUTE FUNCTION analytics_record_configuration_delete();
