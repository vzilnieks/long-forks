
docker compose down
docker volume rm $(docker volume ls -q | grep -E 'docker-bayese_signer') || true
docker volume rm $(docker volume ls -q | grep -E 'docker-bayese_pgdata$') || true
docker volume rm $(docker volume ls -q | grep -E 'docker-bayese_chain$') || true
docker volume rm $(docker volume ls -q | grep -E 'docker-bayese_kafkadata$') || true


# docker-bayese_chain
# docker-bayese_kafkadata
# docker-bayese_pgdata
# docker-bayese_signer1_data
# docker-bayese_signer2_data
# docker-bayese_signer3_data
# docker-bayese_signer4_data
# docker-bayese_signer5_data
