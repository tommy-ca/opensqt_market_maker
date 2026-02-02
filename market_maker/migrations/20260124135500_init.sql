-- Create "state" table
CREATE TABLE `state` (`id` integer NOT NULL, `data` text NOT NULL, `checksum` blob NOT NULL, `updated_at` integer NOT NULL, PRIMARY KEY (`id`), CONSTRAINT `id_check` CHECK (id = 1));
