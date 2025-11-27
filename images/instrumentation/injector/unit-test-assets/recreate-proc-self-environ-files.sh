#!/usr/bin/env bash

# SPDX-FileCopyrightText: Copyright 2025 Dash0 Inc.
# SPDX-License-Identifier: Apache-2.0

set -euo pipefail

cd "$(dirname "${BASH_SOURCE[0]}")"

# Note: Running with env -i to not leak private env vars into the resulting files.
env -i ENV_VAR_1=value_1 ENV_VAR_2=value_2                                           cat /proc/self/environ > proc-self-environ-no-log-level
env -i ENV_VAR_1=value_1 DASH0_INJECTOR_LOG_LEVEL=debug ENV_VAR_2=value_2            cat /proc/self/environ > proc-self-environ-log-level-debug
env -i DASH0_INJECTOR_LOG_LEVEL=Info ENV_VAR_1=value_1 ENV_VAR_2=value_2             cat /proc/self/environ > proc-self-environ-log-level-info
env -i ENV_VAR_1=value_1 ENV_VAR_2=value_2 DASH0_INJECTOR_LOG_LEVEL=WARN             cat /proc/self/environ > proc-self-environ-log-level-warn
env -i ENV_VAR_1=value_1 DASH0_INJECTOR_LOG_LEVEL=error ENV_VAR_2=value_2            cat /proc/self/environ > proc-self-environ-log-level-error
env -i ENV_VAR_1=value_1 DASH0_INJECTOR_LOG_LEVEL=nOnE  ENV_VAR_2=value_2            cat /proc/self/environ > proc-self-environ-log-level-none
env -i ENV_VAR_1=value_1 DASH0_INJECTOR_LOG_LEVEL=arbitrary-string ENV_VAR_2=value_2 cat /proc/self/environ > proc-self-environ-log-level-arbitrary-string

# A carefully constructed (albeit really contrived) test to make sure we handle overly long environment variables
# correctly in print.zig#initLogLevelFromEnvironFile.
env -i \
  VERY_LONG_ENV_VAR=ooobfpukjahymozzkhlgvnbfzblbyavesixpudbodfukxrxuohstkmtkszsffkgcuhyettbsotqcryxnbszxabtoumehnnpzkmfvhnqtrsqvgrgwkmyfzlmqzgwtqyrhmjorflxpfckhfrxbjyyuihjzfjliswdkonqlymawlqboaazfevmklitjaxqcigstvzarkcrqkcmmlsforhbacurdnpdezxzzbulexakrugskusexhtpvzksgcvizgyfwgmwmtlpkvvpjgtmxymxumwqyegzjflfbknowkgxfmuybznmnvitwqnrjokyojgzpvcpwfhgdwrsegnghwtmikycdkjvvbdgmlhsxbrkrhsldtcqybkxkbmclfbugtcrgsdwbcbyzfzcjgotykhrqutodftvzvwbuesylbvaexihdcwecttooafsiupwpkeinixldxjkeyciunacsxlkrsjdcbyojbiixzpesroulnamxbpqvbjpcarkxmssfdjtjcuqnsrjjkfbfgkrohfzawniwuzohepjrdryldtdjmieggoznzscbuestckasbglwqxajsaodmybkxjpknaubtejguxzsiasfqjpajbkrwhfemfnzlioalnynrnvgrxxfilhukzeweneoqycptqodehhtwqtmgxfapnvuhfoeznbpbyoxulutinjpuecfuembpsoichuykkddhebzskljrtwaeldnjpqjsptuzadzzagordbpmuukcdtzstjvmbyrunhkkbmbvbgvmsjguxunfiqegqpbihbxducnwqpcidnjdzfvyxzwvtalwuiixgwqnnguyfivbfaxdtdexhghgrtyomgxbihzduqdlvvortavvxgnhwxfbqjyhwoqnktatmisdqxqeakxdifxnqzyzcpymhinjatabqumdhmrhlwdrihpjrfeytbzahqcplyrukfwftjcgohjwhemyvwlwtrnsstazcuhjhsncycmmuydcenwhzagdsmrotyntushnokphxdgdxmurlyoikyizgcpvusdlbzlaiuxzaputvuaehnqaqsieohngzjzqfmjxvcxdinpmrvcgvwrbadhjemxjuflnpfdcprwrxjvnhorntlzkpgzqqwzcudtswcbifehjzwuhlcccmbdgiiombxaerdblooglgsycptaiawkrwfderlsiisukxhnphniaajboloonqkrwrbvmyrlrtcxpgdjkrfhhnndcthgsmtzwfpbtdsyccdtxnbfnguzbtsnzwyxivtoboxbdjrjqemtrpopzgokhazjuwyoxubvlhtzazgqijjfxijmnozavgrcxygyrvehqfzhuxrvvycepxsgjddassfsfhtnvzfnewzpbkbromggmtjslopfenkdqjqlkbwjgazbrszifosugnklvqymjtvmcmokefeutgkitnjyllfcekwugdqqmukkybnzcxlwbsuiuhuediovufletnlhelwedzkcktetidbjcgzeujzpklrjtrkkpzixhsbqhmtkuukxmujxgrjaijmkqnvtftpgzrpcdlabesrbsqanqbfyshocoxnlyqqsxgmzcprmnhgvubyptwcyxhihjpfpuklszumrhnzpkprucfzsuiipagaiogeaktbbneufnmvqjrhsnjjnehqzfnbjztcfigapdorqmpayodgxbajzyhxxrwdolpzcbkowivqyplfnawdrrjkunvgzbjinpxefsocugdckzwsaovboilvfowmihyocyyculwalqpmbzynqpcqayjtwbtxvjyrbvuicltgfjnrklppefofcprgmwtrgttqzjgkkgijgwivkszyawwkphhaooomsjjntjqshfdimxsmxxyhzalgwcfrhdznxrcpxbqghpecchetegibirnfblcgxlgfesopnivqnrcuhpkqrwmzprobinsxsshyimznypazhmzrhctiakkmskbysidunzfmtfpkcbuojbrzmyuhnfqzfiiDASH0_INJECTOR_LOG_LEVEL=debug \
  DASH0_INJECTOR_LOG_LEVEL=none \
  cat /proc/self/environ > proc-self-environ-log-level-none-overly-long-env-var
