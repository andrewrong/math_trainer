-- The 2026-09-04 import was created while enrichment was unavailable. Without
-- pinyin, the local syllable audio path cannot be resolved. Repair this known
-- household record without replacing any parent-entered non-empty fields.
UPDATE chinese_items SET
    pinyin='gěi', tone='3', meaning='give; for', words_json='["给你","送给"]',
    example_sentence='我把书给你。', pinyin_hint='g + ei, third tone', shape_hint='纟 on the left, 合 on the right'
WHERE week_id='zh-1788766251144717000' AND character='给' AND TRIM(COALESCE(pinyin,''))='';
UPDATE chinese_items SET
    pinyin='xīn', tone='1', meaning='new', words_json='["新书","新年"]',
    example_sentence='我有一本新书。', pinyin_hint='x + in, first tone', shape_hint='亲 without the last dot'
WHERE week_id='zh-1788766251144717000' AND character='新' AND TRIM(COALESCE(pinyin,''))='';
UPDATE chinese_items SET
    pinyin='zuò', tone='4', meaning='do; work', words_json='["作业","工作"]',
    example_sentence='我先做作业。', pinyin_hint='z + uo, falling tone', shape_hint='亻 on the left, 乍 on the right'
WHERE week_id='zh-1788766251144717000' AND character='作' AND TRIM(COALESCE(pinyin,''))='';
UPDATE chinese_items SET
    pinyin='yè', tone='4', meaning='work; study', words_json='["作业","职业"]',
    example_sentence='今天的作业不多。', pinyin_hint='y + e, falling tone', shape_hint='业 has three short strokes at the top'
WHERE week_id='zh-1788766251144717000' AND character='业' AND TRIM(COALESCE(pinyin,''))='';
UPDATE chinese_items SET
    pinyin='lóng', tone='2', meaning='dragon', words_json='["龙舟","恐龙"]',
    example_sentence='龙舟在水上比赛。', pinyin_hint='l + ong, rising tone', shape_hint='Notice the long curved final stroke'
WHERE week_id='zh-1788766251144717000' AND character='龙' AND TRIM(COALESCE(pinyin,''))='';
UPDATE chinese_items SET
    pinyin='pǐ', tone='3', meaning='a measure word for horses; match', words_json='["一匹马","匹配"]',
    example_sentence='草地上有一匹马。', pinyin_hint='p + i, third tone', shape_hint='匚 around 儿'
WHERE week_id='zh-1788766251144717000' AND character='匹' AND TRIM(COALESCE(pinyin,''))='';

UPDATE chinese_review_catalog SET
    pinyin='gěi', tone='3', meaning='give; for', words_json='["给你","送给"]', example_sentence='我把书给你。', pinyin_hint='g + ei, third tone', shape_hint='纟 on the left, 合 on the right'
WHERE character='给' AND TRIM(COALESCE(pinyin,''))='';
UPDATE chinese_review_catalog SET
    pinyin='xīn', tone='1', meaning='new', words_json='["新书","新年"]', example_sentence='我有一本新书。', pinyin_hint='x + in, first tone', shape_hint='亲 without the last dot'
WHERE character='新' AND TRIM(COALESCE(pinyin,''))='';
UPDATE chinese_review_catalog SET
    pinyin='zuò', tone='4', meaning='do; work', words_json='["作业","工作"]', example_sentence='我先做作业。', pinyin_hint='z + uo, falling tone', shape_hint='亻 on the left, 乍 on the right'
WHERE character='作' AND TRIM(COALESCE(pinyin,''))='';
UPDATE chinese_review_catalog SET
    pinyin='pǐ', tone='3', meaning='a measure word for horses; match', words_json='["一匹马","匹配"]', example_sentence='草地上有一匹马。', pinyin_hint='p + i, third tone', shape_hint='匚 around 儿'
WHERE character='匹' AND TRIM(COALESCE(pinyin,''))='';

INSERT OR IGNORE INTO schema_migrations(version) VALUES(16);
