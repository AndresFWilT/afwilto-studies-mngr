-- Spec 17 FR-10: seed TI → Networks → CIDR with one study note that links to
-- the slab CIDR Calculator. Data only, no schema change.
--
-- Find-or-create keyed by ALIVE names, so the owner's existing rows (e.g. a
-- live "TI" root) are reused and never modified. Rows this migration creates
-- carry fixed ids (…-8017-…) so the down migration can logically delete
-- exactly them; on a later up, those ids are revived instead of colliding.
DO $seed$
DECLARE
    v_ti       uuid;
    v_net      uuid;
    v_subject  uuid;
    v_kind     uuid;
    tag_name   text;
    seeded_tag uuid;
    seed_note  constant uuid := '00000000-0000-7000-8017-000000000004';
    note_title constant text := 'IPv4 and IPv6 CIDR — guide and tables';
BEGIN
    SELECT id INTO v_ti FROM categories
     WHERE parent_id IS NULL AND lower(name) = 'ti' AND deleted_at IS NULL;
    IF v_ti IS NULL THEN
        v_ti := '00000000-0000-7000-8017-000000000001';
        INSERT INTO categories (id, parent_id, name, color)
        VALUES (v_ti, NULL, 'TI', '#64748B')
        ON CONFLICT (id) DO UPDATE SET deleted_at = NULL, parent_id = NULL, updated_at = now()
         WHERE categories.deleted_at IS NOT NULL;
    END IF;

    SELECT id INTO v_net FROM categories
     WHERE parent_id = v_ti AND lower(name) = 'networks' AND deleted_at IS NULL;
    IF v_net IS NULL THEN
        v_net := '00000000-0000-7000-8017-000000000002';
        INSERT INTO categories (id, parent_id, name, color)
        VALUES (v_net, v_ti, 'Networks', '#0EA5E9')
        ON CONFLICT (id) DO UPDATE SET deleted_at = NULL, parent_id = EXCLUDED.parent_id, updated_at = now()
         WHERE categories.deleted_at IS NOT NULL;
    END IF;

    SELECT id INTO v_subject FROM subjects
     WHERE category_id = v_net AND lower(name) = 'cidr' AND deleted_at IS NULL;
    IF v_subject IS NULL THEN
        v_subject := '00000000-0000-7000-8017-000000000003';
        INSERT INTO subjects (id, category_id, name, description)
        VALUES (v_subject, v_net, 'CIDR', 'Classless Inter-Domain Routing for IPv4 and IPv6.')
        ON CONFLICT (id) DO UPDATE SET deleted_at = NULL, category_id = EXCLUDED.category_id, updated_at = now()
         WHERE subjects.deleted_at IS NOT NULL;
    END IF;

    -- The owner's note with this title wins: nothing below runs.
    IF EXISTS (SELECT 1 FROM notes WHERE subject_id = v_subject AND lower(title) = lower(note_title) AND deleted_at IS NULL) THEN
        RETURN;
    END IF;

    SELECT id INTO v_kind FROM note_kinds WHERE lower(name) = 'summary' AND deleted_at IS NULL;
    IF v_kind IS NULL THEN
        SELECT id INTO v_kind FROM note_kinds WHERE deleted_at IS NULL ORDER BY lower(name), id LIMIT 1;
    END IF;
    IF v_kind IS NULL THEN
        v_kind := '00000000-0000-7000-8017-000000000005';
        INSERT INTO note_kinds (id, name) VALUES (v_kind, 'Summary')
        ON CONFLICT (id) DO UPDATE SET deleted_at = NULL, updated_at = now()
         WHERE note_kinds.deleted_at IS NOT NULL;
    END IF;

    INSERT INTO notes (id, subject_id, kind_id, title, slab_links, content)
    VALUES (seed_note, v_subject, v_kind, note_title,
            '[{"label": "CIDR Calculator", "path": "/slab/cidr"}]',
            $cidr_note$Classless Inter-Domain Routing in one page: what the `/n` means, how IPv4 masks and IPv6
prefixes split an address, where each block size is used, and the full reference tables.
Try any example in the [CIDR Calculator](/slab/cidr).

## What CIDR is

CIDR replaced the fixed class A / B / C networks in 1993 (today's reference is RFC 4632).
An address is written as `address/n`: the first **n bits** are the **network prefix** and the
remaining bits number the **hosts** inside that network. Because `n` can be any length, a
network gets exactly the size it needs, and neighbouring networks can be advertised as one
shorter prefix.

- A block of prefix `n` holds $2^{32-n}$ IPv4 addresses or $2^{128-n}$ IPv6 addresses.
- `10.0.0.5/24` and `10.0.0.0/24` describe the same block: host bits are ignored when you
  look at the network.

## IPv4: network and host portions

A **subnet mask** is `n` ones followed by `32 − n` zeros. Writing it as a dotted quad gives the
familiar masks: `/24` → `255.255.255.0`. AND-ing an address with the mask yields the network
address; the inverted mask is the **wildcard** (used by ACLs), `0.0.0.255` for a `/24`.

```text
192.168.100.10  = 11000000.10101000.01100100.00001010
255.255.255.0   = 11111111.11111111.11111111.00000000   (/24)
network (AND)   = 11000000.10101000.01100100.00000000   = 192.168.100.0
```

Usable hosts: every IPv4 block reserves its first address (network) and its last
(broadcast), so a `/n` offers $2^{32-n} - 2$ hosts — except `/31`, which RFC 3021 lets
point-to-point links use whole (2 hosts), and `/32`, a single host.

| Prefix | Mask | Usable hosts | Typical use |
|---|---|---|---|
| /30 | 255.255.255.252 | 2 | point-to-point link (classic) |
| /24 | 255.255.255.0 | 254 | small or medium LAN |
| /16 | 255.255.0.0 | 65,534 | large campus network |

## IPv6: prefixes, not masks

IPv6 addresses are 128 bits and use **prefix length only** — there are no dotted masks. The
address space is so large that subnetting is about clean organisation, not saving addresses.

- **/64 is the LAN size.** The first four hextets are the network, the last 64 bits identify
  the interface; SLAAC (stateless address autoconfiguration) relies on that split.
  Example: `2001:db8:85a3::/64` — one segment with $2^{64}$ addresses.
- **/48 is a site.** An organisation receiving `2001:db8:85a3::/48` can carve
  $2^{64-48} = 65{,}536$ separate `/64` LANs from it.

```mermaid
flowchart LR
  site["2001:db8:85a3::/48 — site"] --> lan0["2001:db8:85a3:0::/64 — LAN 0"]
  site --> lan1["2001:db8:85a3:1::/64 — LAN 1"]
  site --> more["… 65,536 /64 LANs in total"]
```

## What CIDR is used for

- **Right-sized allocation:** hand out a `/27` (32 addresses) instead of a whole class C.
- **Route aggregation (supernetting):** `192.168.0.0/24` … `192.168.3.0/24` are advertised as
  the single route `192.168.0.0/22`, keeping routing tables small.
- **Security policy:** firewalls, ACLs and cloud security groups match whole ranges with one
  CIDR rule.
- **Subnetting:** IPv4 splits a block into smaller ones per team or VLAN; IPv6 gives each
  segment a standard `/64`.

## IPv4 CIDR table

Host bits = `32 − prefix`; every value below was computed as $2^{\text{host bits}}$.

| Host bits | CIDR | Subnet mask | Addresses |
|---|---|---|---|
| 0 | /32 | 255.255.255.255 | 1 |
| 1 | /31 | 255.255.255.254 | 2 |
| 2 | /30 | 255.255.255.252 | 4 |
| 3 | /29 | 255.255.255.248 | 8 |
| 4 | /28 | 255.255.255.240 | 16 |
| 5 | /27 | 255.255.255.224 | 32 |
| 6 | /26 | 255.255.255.192 | 64 |
| 7 | /25 | 255.255.255.128 | 128 |
| 8 | /24 | 255.255.255.0 | 256 |
| 9 | /23 | 255.255.254.0 | 512 |
| 10 | /22 | 255.255.252.0 | 1,024 |
| 11 | /21 | 255.255.248.0 | 2,048 |
| 12 | /20 | 255.255.240.0 | 4,096 |
| 13 | /19 | 255.255.224.0 | 8,192 |
| 14 | /18 | 255.255.192.0 | 16,384 |
| 15 | /17 | 255.255.128.0 | 32,768 |
| 16 | /16 | 255.255.0.0 | 65,536 |
| 17 | /15 | 255.254.0.0 | 131,072 |
| 18 | /14 | 255.252.0.0 | 262,144 |
| 19 | /13 | 255.248.0.0 | 524,288 |
| 20 | /12 | 255.240.0.0 | 1,048,576 |
| 21 | /11 | 255.224.0.0 | 2,097,152 |
| 22 | /10 | 255.192.0.0 | 4,194,304 |
| 23 | /9 | 255.128.0.0 | 8,388,608 |
| 24 | /8 | 255.0.0.0 | 16,777,216 |
| 25 | /7 | 254.0.0.0 | 33,554,432 |
| 26 | /6 | 252.0.0.0 | 67,108,864 |
| 27 | /5 | 248.0.0.0 | 134,217,728 |
| 28 | /4 | 240.0.0.0 | 268,435,456 |
| 29 | /3 | 224.0.0.0 | 536,870,912 |
| 30 | /2 | 192.0.0.0 | 1,073,741,824 |
| 31 | /1 | 128.0.0.0 | 2,147,483,648 |
| 32 | /0 | 0.0.0.0 | 4,294,967,296 |

## IPv6 CIDR table

Host bits = `128 − prefix`; addresses = $2^{\text{host bits}}$.

| Host bits | CIDR | Addresses |
|---|---|---|
| 0 | /128 | 1 |
| 1 | /127 | 2 |
| 2 | /126 | 4 |
| 3 | /125 | 8 |
| 4 | /124 | 16 |
| 5 | /123 | 32 |
| 6 | /122 | 64 |
| 7 | /121 | 128 |
| 8 | /120 | 256 |
| 9 | /119 | 512 |
| 10 | /118 | 1,024 |
| 11 | /117 | 2,048 |
| 12 | /116 | 4,096 |
| 13 | /115 | 8,192 |
| 14 | /114 | 16,384 |
| 15 | /113 | 32,768 |
| 16 | /112 | 65,536 |
| 17 | /111 | 131,072 |
| 18 | /110 | 262,144 |
| 19 | /109 | 524,288 |
| 20 | /108 | 1,048,576 |
| 21 | /107 | 2,097,152 |
| 22 | /106 | 4,194,304 |
| 23 | /105 | 8,388,608 |
| 24 | /104 | 16,777,216 |
| 25 | /103 | 33,554,432 |
| 26 | /102 | 67,108,864 |
| 27 | /101 | 134,217,728 |
| 28 | /100 | 268,435,456 |
| 29 | /99 | 536,870,912 |
| 30 | /98 | 1,073,741,824 |
| 31 | /97 | 2,147,483,648 |
| 32 | /96 | 4,294,967,296 |
| 33 | /95 | 8,589,934,592 |
| 34 | /94 | 17,179,869,184 |
| 35 | /93 | 34,359,738,368 |
| 36 | /92 | 68,719,476,736 |
| 37 | /91 | 137,438,953,472 |
| 38 | /90 | 274,877,906,944 |
| 39 | /89 | 549,755,813,888 |
| 40 | /88 | 1,099,511,627,776 |
| 41 | /87 | 2,199,023,255,552 |
| 42 | /86 | 4,398,046,511,104 |
| 43 | /85 | 8,796,093,022,208 |
| 44 | /84 | 17,592,186,044,416 |
| 45 | /83 | 35,184,372,088,832 |
| 46 | /82 | 70,368,744,177,664 |
| 47 | /81 | 140,737,488,355,328 |
| 48 | /80 | 281,474,976,710,656 |
| 49 | /79 | 562,949,953,421,312 |
| 50 | /78 | 1,125,899,906,842,624 |
| 51 | /77 | 2,251,799,813,685,248 |
| 52 | /76 | 4,503,599,627,370,496 |
| 53 | /75 | 9,007,199,254,740,992 |
| 54 | /74 | 18,014,398,509,481,984 |
| 55 | /73 | 36,028,797,018,963,968 |
| 56 | /72 | 72,057,594,037,927,936 |
| 57 | /71 | 144,115,188,075,855,872 |
| 58 | /70 | 288,230,376,151,711,744 |
| 59 | /69 | 576,460,752,303,423,488 |
| 60 | /68 | 1,152,921,504,606,846,976 |
| 61 | /67 | 2,305,843,009,213,693,952 |
| 62 | /66 | 4,611,686,018,427,387,904 |
| 63 | /65 | 9,223,372,036,854,775,808 |
| 64 | /64 | 18,446,744,073,709,551,616 |
| 65 | /63 | 36,893,488,147,419,103,232 |
| 66 | /62 | 73,786,976,294,838,206,464 |
| 67 | /61 | 147,573,952,589,676,412,928 |
| 68 | /60 | 295,147,905,179,352,825,856 |
| 69 | /59 | 590,295,810,358,705,651,712 |
| 70 | /58 | 1,180,591,620,717,411,303,424 |
| 71 | /57 | 2,361,183,241,434,822,606,848 |
| 72 | /56 | 4,722,366,482,869,645,213,696 |
| 73 | /55 | 9,444,732,965,739,290,427,392 |
| 74 | /54 | 18,889,465,931,478,580,854,784 |
| 75 | /53 | 37,778,931,862,957,161,709,568 |
| 76 | /52 | 75,557,863,725,914,323,419,136 |
| 77 | /51 | 151,115,727,451,828,646,838,272 |
| 78 | /50 | 302,231,454,903,657,293,676,544 |
| 79 | /49 | 604,462,909,807,314,587,353,088 |
| 80 | /48 | 1,208,925,819,614,629,174,706,176 |
| 81 | /47 | 2,417,851,639,229,258,349,412,352 |
| 82 | /46 | 4,835,703,278,458,516,698,824,704 |
| 83 | /45 | 9,671,406,556,917,033,397,649,408 |
| 84 | /44 | 19,342,813,113,834,066,795,298,816 |
| 85 | /43 | 38,685,626,227,668,133,590,597,632 |
| 86 | /42 | 77,371,252,455,336,267,181,195,264 |
| 87 | /41 | 154,742,504,910,672,534,362,390,528 |
| 88 | /40 | 309,485,009,821,345,068,724,781,056 |
| 89 | /39 | 618,970,019,642,690,137,449,562,112 |
| 90 | /38 | 1,237,940,039,285,380,274,899,124,224 |
| 91 | /37 | 2,475,880,078,570,760,549,798,248,448 |
| 92 | /36 | 4,951,760,157,141,521,099,596,496,896 |
| 93 | /35 | 9,903,520,314,283,042,199,192,993,792 |
| 94 | /34 | 19,807,040,628,566,084,398,385,987,584 |
| 95 | /33 | 39,614,081,257,132,168,796,771,975,168 |
| 96 | /32 | 79,228,162,514,264,337,593,543,950,336 |
| 97 | /31 | 158,456,325,028,528,675,187,087,900,672 |
| 98 | /30 | 316,912,650,057,057,350,374,175,801,344 |
| 99 | /29 | 633,825,300,114,114,700,748,351,602,688 |
| 100 | /28 | 1,267,650,600,228,229,401,496,703,205,376 |
| 101 | /27 | 2,535,301,200,456,458,802,993,406,410,752 |
| 102 | /26 | 5,070,602,400,912,917,605,986,812,821,504 |
| 103 | /25 | 10,141,204,801,825,835,211,973,625,643,008 |
| 104 | /24 | 20,282,409,603,651,670,423,947,251,286,016 |
| 105 | /23 | 40,564,819,207,303,340,847,894,502,572,032 |
| 106 | /22 | 81,129,638,414,606,681,695,789,005,144,064 |
| 107 | /21 | 162,259,276,829,213,363,391,578,010,288,128 |
| 108 | /20 | 324,518,553,658,426,726,783,156,020,576,256 |
| 109 | /19 | 649,037,107,316,853,453,566,312,041,152,512 |
| 110 | /18 | 1,298,074,214,633,706,907,132,624,082,305,024 |
| 111 | /17 | 2,596,148,429,267,413,814,265,248,164,610,048 |
| 112 | /16 | 5,192,296,858,534,827,628,530,496,329,220,096 |
| 113 | /15 | 10,384,593,717,069,655,257,060,992,658,440,192 |
| 114 | /14 | 20,769,187,434,139,310,514,121,985,316,880,384 |
| 115 | /13 | 41,538,374,868,278,621,028,243,970,633,760,768 |
| 116 | /12 | 83,076,749,736,557,242,056,487,941,267,521,536 |
| 117 | /11 | 166,153,499,473,114,484,112,975,882,535,043,072 |
| 118 | /10 | 332,306,998,946,228,968,225,951,765,070,086,144 |
| 119 | /9 | 664,613,997,892,457,936,451,903,530,140,172,288 |
| 120 | /8 | 1,329,227,995,784,915,872,903,807,060,280,344,576 |
| 121 | /7 | 2,658,455,991,569,831,745,807,614,120,560,689,152 |
| 122 | /6 | 5,316,911,983,139,663,491,615,228,241,121,378,304 |
| 123 | /5 | 10,633,823,966,279,326,983,230,456,482,242,756,608 |
| 124 | /4 | 21,267,647,932,558,653,966,460,912,964,485,513,216 |
| 125 | /3 | 42,535,295,865,117,307,932,921,825,928,971,026,432 |
| 126 | /2 | 85,070,591,730,234,615,865,843,651,857,942,052,864 |
| 127 | /1 | 170,141,183,460,469,231,731,687,303,715,884,105,728 |
| 128 | /0 | 340,282,366,920,938,463,463,374,607,431,768,211,456 |

> **Correction:** the source article lists `/78` as 1,125,899,906,842,620. The exact value is
> $2^{50}$ = **1,125,899,906,842,624** (the table above is computed, not copied).

## Source

Summarised in my own words from *"A Comprehensive Guide to IPv4 and IPv6 CIDR"*,
IP2Location Articles & Tutorials (hexasoft, updated 2026-06-05). Tables recomputed
independently.$cidr_note$)
    ON CONFLICT (id) DO UPDATE
       SET deleted_at = NULL, subject_id = EXCLUDED.subject_id, kind_id = EXCLUDED.kind_id, updated_at = now()
     WHERE notes.deleted_at IS NOT NULL;

    FOREACH tag_name IN ARRAY ARRAY['cidr', 'networking', 'ipv4', 'ipv6', 'subnetting'] LOOP
        INSERT INTO tags (name) VALUES (tag_name) ON CONFLICT (name) DO NOTHING;
        SELECT id INTO seeded_tag FROM tags WHERE name = tag_name;
        INSERT INTO note_tags (note_id, tag_id) VALUES (seed_note, seeded_tag)
        ON CONFLICT (note_id, tag_id) DO UPDATE SET deleted_at = NULL;
    END LOOP;
END
$seed$;
