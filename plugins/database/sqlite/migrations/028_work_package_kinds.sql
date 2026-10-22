ALTER TABLE work_packages ADD COLUMN kind TEXT NOT NULL DEFAULT 'directory' CHECK(kind IN ('directory','cd_dump'));
CREATE INDEX work_packages_kind ON work_packages(owner,work_id,kind);

-- 旧包与导入回执都是直接保存的包文档，私有绑定同处根对象。
UPDATE work_packages SET document=json_set(document,'$.kind','directory');
UPDATE idempotency SET result=json_set(result,'$.kind','directory')
 WHERE json_valid(result)
 AND route='works/' || json_extract(result,'$.workId') || '/packages/import'
 AND json_type(result,'$.packageId')='text'
 AND json_type(result,'$.nodes')='array';
